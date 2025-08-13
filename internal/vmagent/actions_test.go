package vmagent

import (
	"context"
	"errors"
	"testing"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Helper function to create a mock matcher for VmCreation that allows flexible node selection
func matchVmCreation(vmid int, name, pool string, cores, memory, disk int, image string) interface{} {
	return mock.MatchedBy(func(vmCreation *VmCreation) bool {
		return vmCreation.VMID == vmid &&
			vmCreation.Name == name &&
			vmCreation.Pool == pool &&
			vmCreation.Sockets == 1 &&
			vmCreation.Cores == cores &&
			vmCreation.Memory == memory &&
			vmCreation.SCSI0 == disk &&
			vmCreation.IDE2 == image &&
			// Allow any reasonable node name since it's randomly selected
			vmCreation.Node != ""
	})
}

func TestExtractUserAndVMID(t *testing.T) {
	tests := []struct {
		name         string
		service      *agent.Service
		expectError  bool
		errorMsg     string
		expectedUser string
		expectedVMID int
	}{
		{
			name: "successful_extraction",
			service: &agent.Service{
				ConsumerID: "testuser",
				Resources: map[string]interface{}{
					"vmid": 123,
				},
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
			service: &agent.Service{
				ConsumerID: "testuser",
				Resources:  nil,
			},
			expectError: true,
			errorMsg:    "job service resources are nil",
		},
		{
			name: "vmid_not_found",
			service: &agent.Service{
				ConsumerID: "testuser",
				Resources:  map[string]interface{}{},
			},
			expectError: true,
			errorMsg:    "vmid not found in job service resources",
		},
		{
			name: "vmid_wrong_type",
			service: &agent.Service{
				ConsumerID: "testuser",
				Resources: map[string]interface{}{
					"vmid": "not-an-int",
				},
			},
			expectError: true,
			errorMsg:    "vmid is not an integer",
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
	require.NotNil(t, response.Resources, "Response resources should not be nil")
	require.NotNil(t, response.ExternalID, "Response external ID should not be nil")

	assert.Equal(t, vmid, response.Resources.VMID, "Resources VMID should match")
	assert.Equal(t, "456", *response.ExternalID, "External ID should match VMID as string")
}

func TestVMAgent_CreateVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.Job[VMServiceProperties]
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_creation",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-1",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
				},
				Params: &VMServiceProperties{
					CPU:    2,
					Memory: 1024,
					Disk:   20,
					Image:  "debian-12-standard.iso",
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextID: 100,
					Nodes:  []string{"node1", "node2"},
					Pools:  []string{"pool1"},
				}
				mockClient.EXPECT().GetUserPools("testuser").Return(userResources, nil).Once()
				// Use MatchedBy to accept any node from the available nodes
				mockClient.EXPECT().CreateVM("testuser", matchVmCreation(100, "test-vm", "pool1", 2, 1024, 20, "debian-12-standard.iso")).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "get_user_pools_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-2",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
				},
				Params: &VMServiceProperties{
					CPU:    1,
					Memory: 512,
					Disk:   10,
					Image:  "alpine.iso",
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().GetUserPools("testuser").Return(nil, errors.New("user not found")).Once()
			},
			expectError: true,
			errorMsg:    "failed to get user resources",
		},
		{
			name: "no_nodes_available",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-3",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
				},
				Params: &VMServiceProperties{
					CPU:    1,
					Memory: 512,
					Disk:   10,
					Image:  "alpine.iso",
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextID: 101,
					Nodes:  []string{}, // No nodes available
					Pools:  []string{"pool1"},
				}
				mockClient.EXPECT().GetUserPools("testuser").Return(userResources, nil).Once()
			},
			expectError: true,
			errorMsg:    "no nodes available for user",
		},
		{
			name: "no_pools_available",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-4",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
				},
				Params: &VMServiceProperties{
					CPU:    1,
					Memory: 512,
					Disk:   10,
					Image:  "alpine.iso",
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextID: 102,
					Nodes:  []string{"node1"},
					Pools:  []string{}, // No pools available
				}
				mockClient.EXPECT().GetUserPools("testuser").Return(userResources, nil).Once()
			},
			expectError: true,
			errorMsg:    "no pools available for user",
		},
		{
			name: "create_vm_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-5",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
				},
				Params: &VMServiceProperties{
					CPU:    1,
					Memory: 512,
					Disk:   10,
					Image:  "alpine.iso",
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextID: 103,
					Nodes:  []string{"node1"},
					Pools:  []string{"pool1"},
				}
				mockClient.EXPECT().GetUserPools("testuser").Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVM("testuser", matchVmCreation(103, "test-vm", "pool1", 1, 512, 10, "alpine.iso")).Return(errors.New("insufficient resources")).Once()
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

			// Get the create VM handler
			createHandler := vmAgent.CreateVMHandler()
			require.NotNil(t, createHandler, "CreateVMHandler should return a non-nil function")

			// Execute the create VM handler
			ctx := context.Background()
			response, err := createHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected create VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, response, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected create VM handler to succeed")
				require.NotNil(t, response, "Response should not be nil")
				require.NotNil(t, response.Resources, "Response resources should not be nil")
				require.NotNil(t, response.ExternalID, "Response external ID should not be nil")

				assert.Equal(t, 100, response.Resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "100", *response.ExternalID, "External ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_UpdateVMHandler(t *testing.T) {
	tests := []struct {
		name          string
		job           *agent.Job[VMServiceProperties]
		setupMock     func(*MockProxmoxClient)
		expectError   bool
		errorMsg      string
		isUnsupported bool
	}{
		{
			name: "successful_vm_update",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-1",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Status:     agent.ServiceStatusStopped,
					Resources: map[string]interface{}{
						"vmid": 200,
					},
				},
				Params: &VMServiceProperties{
					CPU:    4,
					Memory: 2048,
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM("testuser", 200, &VmPatch{
					Cores:  4,
					Memory: 2048,
				}).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "vm_not_stopped",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-2",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Status:     "running", // Not stopped - using string literal since ServiceStatusRunning is undefined
					Resources: map[string]interface{}{
						"vmid": 201,
					},
				},
				Params: &VMServiceProperties{
					CPU:    2,
					Memory: 1024,
				},
			},
			setupMock:     func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError:   true,
			isUnsupported: true,
			errorMsg:      "cannot update a VM in status",
		},
		{
			name: "extract_vmid_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-3",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Status:     agent.ServiceStatusStopped,
					Resources:  map[string]interface{}{}, // Missing vmid
				},
				Params: &VMServiceProperties{
					CPU:    2,
					Memory: 1024,
				},
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "failed to extract VMID",
		},
		{
			name: "patch_vm_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-4",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Status:     agent.ServiceStatusStopped,
					Resources: map[string]interface{}{
						"vmid": 202,
					},
				},
				Params: &VMServiceProperties{
					CPU:    8,
					Memory: 4096,
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM("testuser", 202, &VmPatch{
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

			// Get the update VM handler
			updateHandler := vmAgent.UpdateVMHandler()
			require.NotNil(t, updateHandler, "UpdateVMHandler should return a non-nil function")

			// Execute the update VM handler
			ctx := context.Background()
			response, err := updateHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected update VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, response, "Response should be nil when error occurs")

				if tt.isUnsupported {
					var unsupportedErr *agent.UnsupportedJobError
					assert.ErrorAs(t, err, &unsupportedErr, "Error should be UnsupportedJobError")
				}
			} else {
				require.NoError(t, err, "Expected update VM handler to succeed")
				require.NotNil(t, response, "Response should not be nil")
				require.NotNil(t, response.Resources, "Response resources should not be nil")
				require.NotNil(t, response.ExternalID, "Response external ID should not be nil")

				assert.Equal(t, 200, response.Resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "200", *response.ExternalID, "External ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_StartVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.Job[VMServiceProperties]
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_start",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-1",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": 300,
					},
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StartVM("testuser", 300).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "extract_vmid_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-2",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources:  nil, // Missing resources
				},
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "failed to extract VMID",
		},
		{
			name: "start_vm_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-3",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": 301,
					},
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StartVM("testuser", 301).Return(errors.New("VM is already running")).Once()
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

			// Get the start VM handler
			startHandler := vmAgent.StartVMHandler()
			require.NotNil(t, startHandler, "StartVMHandler should return a non-nil function")

			// Execute the start VM handler
			ctx := context.Background()
			response, err := startHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected start VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, response, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected start VM handler to succeed")
				require.NotNil(t, response, "Response should not be nil")
				require.NotNil(t, response.Resources, "Response resources should not be nil")
				require.NotNil(t, response.ExternalID, "Response external ID should not be nil")

				assert.Equal(t, 300, response.Resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "300", *response.ExternalID, "External ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_StopVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.Job[VMServiceProperties]
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_stop",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-1",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": 400,
					},
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StopVM("testuser", 400).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "extract_vmid_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-2",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": "invalid", // Wrong type
					},
				},
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "failed to extract VMID",
		},
		{
			name: "stop_vm_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-3",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": 401,
					},
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().StopVM("testuser", 401).Return(errors.New("VM is already stopped")).Once()
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

			// Get the stop VM handler
			stopHandler := vmAgent.StopVMHandler()
			require.NotNil(t, stopHandler, "StopVMHandler should return a non-nil function")

			// Execute the stop VM handler
			ctx := context.Background()
			response, err := stopHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected stop VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, response, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected stop VM handler to succeed")
				require.NotNil(t, response, "Response should not be nil")
				require.NotNil(t, response.Resources, "Response resources should not be nil")
				require.NotNil(t, response.ExternalID, "Response external ID should not be nil")

				assert.Equal(t, 400, response.Resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "400", *response.ExternalID, "External ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_DeleteVMHandler(t *testing.T) {
	tests := []struct {
		name        string
		job         *agent.Job[VMServiceProperties]
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_vm_delete",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-1",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": 500,
					},
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().DeleteVM("testuser", 500).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name: "extract_vmid_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-2",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources:  map[string]interface{}{}, // Missing vmid
				},
			},
			setupMock:   func(mockClient *MockProxmoxClient) {}, // No mock calls expected
			expectError: true,
			errorMsg:    "failed to extract VMID",
		},
		{
			name: "delete_vm_fails",
			job: &agent.Job[VMServiceProperties]{
				ID: "test-job-3",
				Service: &agent.Service{
					ConsumerID: "testuser",
					Name:       "test-vm",
					Resources: map[string]interface{}{
						"vmid": 501,
					},
				},
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().DeleteVM("testuser", 501).Return(errors.New("VM has snapshots")).Once()
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

			// Get the delete VM handler
			deleteHandler := vmAgent.DeleteVMHandler()
			require.NotNil(t, deleteHandler, "DeleteVMHandler should return a non-nil function")

			// Execute the delete VM handler
			ctx := context.Background()
			response, err := deleteHandler(ctx, tt.job)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected delete VM handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, response, "Response should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected delete VM handler to succeed")
				require.NotNil(t, response, "Response should not be nil")
				require.NotNil(t, response.Resources, "Response resources should not be nil")
				require.NotNil(t, response.ExternalID, "Response external ID should not be nil")

				assert.Equal(t, 500, response.Resources.VMID, "Response VMID should match expected")
				assert.Equal(t, "500", *response.ExternalID, "External ID should match VMID as string")
			}
		})
	}
}
