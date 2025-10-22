package vmagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Test constants for consistent test user IDs
const testConsumerID = "testuser"

var (
	testUserID, testPoolID = MakeUserIDs(testConsumerID)
)

// Helper function to create a mock matcher for VmCreation that allows flexible node selection
// TODO Phase 2: Replace with matchVmCreateFromTemplate
func matchVmCreation(vmid int, name, pool string, cores, memory, disk int, template string) interface{} {
	return mock.MatchedBy(func(vmCreation *VmCreation) bool {
		return vmCreation.VMID == vmid &&
			vmCreation.Name == name &&
			vmCreation.Pool == pool &&
			vmCreation.Sockets == 1 &&
			vmCreation.Cores == cores &&
			vmCreation.Memory == memory &&
			vmCreation.Disk == disk &&
			vmCreation.Image == template &&
			// Allow any reasonable node name since it's randomly selected
			vmCreation.Node != ""
	})
}

// Helper function to create raw params from VMProperties
func rawParamsPtr(props VMProperties) *json.RawMessage {
	data, err := json.Marshal(props)
	if err != nil {
		panic(err) // This should never happen in tests
	}
	raw := json.RawMessage(data)
	return &raw
}

func TestExtractUserAndVMID(t *testing.T) {
	tests := []struct {
		name         string
		service      *agent.RawService
		expectError  bool
		errorMsg     string
		expectedUser string
		expectedVMID int
	}{
		{
			name: "successful_extraction",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{"vmid": 123}`),
			},
			expectError:  false,
			expectedUser: "testuser",
			expectedVMID: 123,
		},
		{
			name:        "service_is_nil",
			service:     nil,
			expectError: true,
			errorMsg:    "job service is nil",
		},
		{
			name: "service_resources_is_nil",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: nil,
			},
			expectError: true,
			errorMsg:    "job service agent instance data are nil",
		},
		{
			name: "invalid_json_resources",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`invalid json`),
			},
			expectError: true,
			errorMsg:    "failed to unmarshal service agent instance data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, vmid, err := extractUserAndVMID(tt.service)

			if tt.expectError {
				require.Error(t, err, "Expected extractUserAndVMID to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Empty(t, user, "User should be empty when error occurs")
				assert.Zero(t, vmid, "VMID should be zero when error occurs")
			} else {
				require.NoError(t, err, "Expected extractUserAndVMID to succeed")
				assert.Equal(t, tt.expectedUser, user, "User should match expected")
				assert.Equal(t, tt.expectedVMID, vmid, "VMID should match expected")
			}
		})
	}
}

func TestVMAgent_NewJobResponse(t *testing.T) {
	vmAgent := &VMAgent{}
	vmid := 456

	response := vmAgent.newJobResponse(vmid)

	require.NotNil(t, response, "Response should not be nil")
	require.NotNil(t, response.AgentInstanceData, "Response agent instance data should not be nil")
	require.NotNil(t, response.AgentInstanceID, "Response agent instance ID should not be nil")

	assert.Equal(t, vmid, response.AgentInstanceData.VMID, "Agent instance data VMID should match")
	assert.Equal(t, "456", *response.AgentInstanceID, "Agent instance ID should match VMID as string")
}

func TestVMAgent_CreateVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.RawJob
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_creation",
			job: &agent.RawJob{
				ID: "test-job-1",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      2,
					Memory:   1024,
					Disk:     20,
					Template: "debian-13",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.100",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "testuser",
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 100,
					Nodes:    []string{"node1", "node2"},
					Pools:    []string{"pool1"},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				// Use MatchedBy to accept any node from the available nodes
				mockClient.EXPECT().CreateVM(testUserID, matchVmCreation(100, "test-vm", "pool1", 2, 1024, 20, "debian-13")).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "get_user_pools_fails",
			job: &agent.RawJob{
				ID: "test-job-2",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "alpine-template",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "testuser",
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().GetUserPools(testUserID).Return(nil, errors.New("user not found")).Once()
				// Expect CreateUser call since GetUserPools fails
				mockClient.EXPECT().CreateUser(&PveUserCreation{
					UserID:        testUserID,
					PoolID:        testPoolID,
					ResetIfExists: false,
				}).Return(errors.New("failed to create user")).Once()
			},
			expectError: true,
			errorMsg:    "failed to create user",
		},
		{
			name: "no_nodes_available",
			job: &agent.RawJob{
				ID: "test-job-3",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "alpine-template",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "testuser",
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 101,
					Nodes:    []string{}, // No nodes available
					Pools:    []string{"pool1"},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
			},
			expectError: true,
			errorMsg:    "no nodes available for user",
		},
		{
			name: "no_pools_available",
			job: &agent.RawJob{
				ID: "test-job-4",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "alpine-template",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "testuser",
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 102,
					Nodes:    []string{"node1"},
					Pools:    []string{}, // No pools available
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
			},
			expectError: true,
			errorMsg:    "no pools available for user",
		},
		{
			name: "create_vm_fails",
			job: &agent.RawJob{
				ID: "test-job-5",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "alpine-template",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "testuser",
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 103,
					Nodes:    []string{"node1"},
					Pools:    []string{"pool1"},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVM(testUserID, matchVmCreation(103, "test-vm", "pool1", 1, 512, 10, "alpine-template")).Return(errors.New("insufficient resources")).Once()
			},
			expectError: true,
			errorMsg:    "failed to create VM",
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

			// Get the create VM handler and wrap it
			typedHandler := vmAgent.CreateVMHandler()
			require.NotNil(t, typedHandler, "CreateVMHandler should return a non-nil function")
			wrappedHandler := agent.JobHandlerWrapper(typedHandler)

			// Execute the wrapped handler
			ctx := context.Background()
			rawResponse, err := wrappedHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected create VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, rawResponse, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected create VM handler to succeed")
				require.NotNil(t, rawResponse, "Response should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceData, "Response agent instance data should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceID, "Response agent instance ID should not be nil")

				// Parse the agent instance data to verify VMID
				var resources VMResources
				err := json.Unmarshal(*rawResponse.AgentInstanceData, &resources)
				require.NoError(t, err, "Should be able to unmarshal agent instance data")
				assert.Equal(t, 100, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "100", *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_UpdateVMHandler(t *testing.T) {
	tests := []struct {
		name          string
		job           *agent.RawJob
		setupMock     func(*MockProxmoxClient)
		expectError   bool
		errorMsg      string
		isUnsupported bool
	}{
		{
			name: "successful_vm_update",
			job: &agent.RawJob{
				ID: "test-job-1",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            agent.ServiceStatusStopped,
					AgentInstanceData: rawResourcesPtr(`{"vmid": 200}`),
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:    4,
					Memory: 2048,
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM(testUserID, 200, &VmPatch{
					Cores:  4,
					Memory: 2048,
				}).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "vm_not_stopped",
			job: &agent.RawJob{
				ID: "test-job-2",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            "running", // Not stopped - using string literal since ServiceStatusRunning is undefined
					AgentInstanceData: rawResourcesPtr(`{"vmid": 201}`),
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:    2,
					Memory: 1024,
				}),
			},
			setupMock:     func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError:   true,
			isUnsupported: true,
			errorMsg:      "cannot update a VM in status",
		},
		{
			name: "extract_vmid_fails",
			job: &agent.RawJob{
				ID: "test-job-3",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            agent.ServiceStatusStopped,
					AgentInstanceData: rawResourcesPtr(`{}`), // Missing vmid
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:    2,
					Memory: 1024,
				}),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "vmid is missing or zero",
		},
		{
			name: "patch_vm_fails",
			job: &agent.RawJob{
				ID: "test-job-4",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            agent.ServiceStatusStopped,
					AgentInstanceData: rawResourcesPtr(`{"vmid": 202}`),
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:    8,
					Memory: 4096,
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM(testUserID, 202, &VmPatch{
					Cores:  8,
					Memory: 4096,
				}).Return(errors.New("VM is locked")).Once()
			},
			expectError: true,
			errorMsg:    "failed to update VM",
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

			// Get the update VM handler and wrap it
			typedHandler := vmAgent.UpdateVMHandler()
			require.NotNil(t, typedHandler, "UpdateVMHandler should return a non-nil function")
			wrappedHandler := agent.JobHandlerWrapper(typedHandler)

			// Execute the wrapped handler
			ctx := context.Background()
			rawResponse, err := wrappedHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected update VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, rawResponse, "Response should be nil when error occurs")

				if tt.isUnsupported {
					// Check that the error message contains "unsupported"
					assert.Contains(t, err.Error(), "unsupported", "Error should indicate unsupported operation")
				}
			} else {
				require.NoError(t, err, "Expected update VM handler to succeed")
				require.NotNil(t, rawResponse, "Response should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceData, "Response agent instance data should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceID, "Response agent instance ID should not be nil")

				// Parse the agent instance data to verify VMID
				var resources VMResources
				err := json.Unmarshal(*rawResponse.AgentInstanceData, &resources)
				require.NoError(t, err, "Should be able to unmarshal agent instance data")
				assert.Equal(t, 200, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "200", *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_StartVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.RawJob
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_start",
			job: &agent.RawJob{
				ID: "test-job-1",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": 300}`),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StartVM(testUserID, 300).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "extract_vmid_fails",
			job: &agent.RawJob{
				ID: "test-job-2",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: nil, // Missing agent instance data
				}),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "vmid is missing or zero",
		},
		{
			name: "start_vm_fails",
			job: &agent.RawJob{
				ID: "test-job-3",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": 301}`),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StartVM(testUserID, 301).Return(errors.New("VM is already running")).Once()
			},
			expectError: true,
			errorMsg:    "failed to start VM",
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

			// Get the start VM handler and wrap it
			typedHandler := vmAgent.StartVMHandler()
			require.NotNil(t, typedHandler, "StartVMHandler should return a non-nil function")
			wrappedHandler := agent.JobHandlerWrapper(typedHandler)

			// Execute the wrapped handler
			ctx := context.Background()
			rawResponse, err := wrappedHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected start VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, rawResponse, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected start VM handler to succeed")
				require.NotNil(t, rawResponse, "Response should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceData, "Response agent instance data should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceID, "Response agent instance ID should not be nil")

				// Parse the agent instance data to verify VMID
				var resources VMResources
				err := json.Unmarshal(*rawResponse.AgentInstanceData, &resources)
				require.NoError(t, err, "Should be able to unmarshal agent instance data")
				assert.Equal(t, 300, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "300", *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_StopVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.RawJob
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_stop",
			job: &agent.RawJob{
				ID: "test-job-1",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": 400}`),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StopVM(testUserID, 400).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "extract_vmid_fails",
			job: &agent.RawJob{
				ID: "test-job-2",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": "invalid"}`), // Wrong type
				}),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "json: cannot unmarshal string into Go struct field VMResources.vmid of type int",
		},
		{
			name: "stop_vm_fails",
			job: &agent.RawJob{
				ID: "test-job-3",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": 401}`),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StopVM(testUserID, 401).Return(errors.New("VM is already stopped")).Once()
			},
			expectError: true,
			errorMsg:    "failed to stop VM",
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

			// Get the stop VM handler and wrap it
			typedHandler := vmAgent.StopVMHandler()
			require.NotNil(t, typedHandler, "StopVMHandler should return a non-nil function")
			wrappedHandler := agent.JobHandlerWrapper(typedHandler)

			// Execute the wrapped handler
			ctx := context.Background()
			rawResponse, err := wrappedHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected stop VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, rawResponse, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected stop VM handler to succeed")
				require.NotNil(t, rawResponse, "Response should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceData, "Response agent instance data should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceID, "Response agent instance ID should not be nil")

				// Parse the agent instance data to verify VMID
				var resources VMResources
				err := json.Unmarshal(*rawResponse.AgentInstanceData, &resources)
				require.NoError(t, err, "Should be able to unmarshal agent instance data")
				assert.Equal(t, 400, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "400", *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_DeleteVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.RawJob
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_delete",
			job: &agent.RawJob{
				ID: "test-job-1",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": 500}`),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().DeleteVM(testUserID, 500).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "extract_vmid_fails",
			job: &agent.RawJob{
				ID: "test-job-2",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{}`), // Missing vmid
				}),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "vmid is missing or zero",
		},
		{
			name: "delete_vm_fails",
			job: &agent.RawJob{
				ID: "test-job-3",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					AgentInstanceData: rawResourcesPtr(`{"vmid": 501}`),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().DeleteVM(testUserID, 501).Return(errors.New("VM has snapshots")).Once()
			},
			expectError: true,
			errorMsg:    "failed to delete VM",
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

			// Get the delete VM handler and wrap it
			typedHandler := vmAgent.DeleteVMHandler()
			require.NotNil(t, typedHandler, "DeleteVMHandler should return a non-nil function")
			wrappedHandler := agent.JobHandlerWrapper(typedHandler)

			// Execute the wrapped handler
			ctx := context.Background()
			rawResponse, err := wrappedHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected delete VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, rawResponse, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected delete VM handler to succeed")
				require.NotNil(t, rawResponse, "Response should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceData, "Response agent instance data should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceID, "Response agent instance ID should not be nil")

				// Parse the agent instance data to verify VMID
				var resources VMResources
				err := json.Unmarshal(*rawResponse.AgentInstanceData, &resources)
				require.NoError(t, err, "Should be able to unmarshal agent instance data")
				assert.Equal(t, 500, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "500", *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
			}
		})
	}
}
