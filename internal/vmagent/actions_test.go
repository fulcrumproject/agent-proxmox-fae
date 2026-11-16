package vmagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Helper function to create a mock matcher for VmCreateFromTemplate that allows flexible node selection
func matchVmCreateFromTemplate(vmid, templateID int, name, pool string, cores, memory, disk int) interface{} {
	return mock.MatchedBy(func(vmReq *VmCreateFromTemplate) bool {
		return vmReq.VMID == vmid &&
			vmReq.TemplateVMID == templateID &&
			vmReq.Name == name &&
			vmReq.Pool == pool &&
			vmReq.Bridge == "vmbr" &&
			vmReq.CPUs == cores &&
			vmReq.Memory == memory &&
			vmReq.DiskSize == disk &&
			// Allow any reasonable node name since it's randomly selected
			vmReq.Node != ""
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

// Helper function to create raw params from VMUpdateProperties
func rawUpdateParamsPtr(props VMUpdateProperties) *json.RawMessage {
	data, err := json.Marshal(props)
	if err != nil {
		panic(err) // This should never happen in tests
	}
	raw := json.RawMessage(data)
	return &raw
}

// Helper functions to create pointers for test data
func intPtr(v int) *int {
	return &v
}

func firewallRulesPtr(rules []FirewallRule) *[]FirewallRule {
	return &rules
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
		name         string
		job          *agent.RawJob
		setupMock    func(*MockProxmoxClient)
		expectError  bool
		errorMsg     string
		expectedVMID int
	}{
		{
			name:         "successful_vm_creation",
			expectedVMID: 100,
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
					Templates: []PveTemplate{
						{ID: 100, Name: "debian-13"},
						{ID: 101, Name: "ubuntu-24.04"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				// Use MatchedBy to accept any node from the available nodes
				mockClient.EXPECT().CreateVMFromTemplate(testUserID, matchVmCreateFromTemplate(100, 100, "test-vm", "pool1", 2, 1024, 20)).Return(nil).Once()
				mockClient.EXPECT().WaitForVMReady(mock.Anything, testUserID, 100, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
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
					UserID: testUserID,
					PoolID: testPoolID,
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
					Templates: []PveTemplate{
						{ID: 100, Name: "alpine-template"},
					},
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
					Templates: []PveTemplate{
						{ID: 100, Name: "alpine-template"},
					},
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
					Templates: []PveTemplate{
						{ID: 100, Name: "alpine-template"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVMFromTemplate(testUserID, matchVmCreateFromTemplate(103, 100, "test-vm", "pool1", 1, 512, 10)).Return(errors.New("insufficient resources")).Once()
			},
			expectError: true,
			errorMsg:    "failed to create VM",
		},
		{
			name: "validation_missing_loginuser",
			job: &agent.RawJob{
				ID: "test-job-6",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "debian-13",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "", // Missing
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				// No mocks needed - validation happens before API calls
			},
			expectError: true,
			errorMsg:    "loginuser is required",
		},
		{
			name: "validation_missing_login_credentials",
			job: &agent.RawJob{
				ID: "test-job-7",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "debian-13",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser:   "testuser",
					LoginPass:   "", // Missing
					LoginSSHKey: "", // Missing
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				// No mocks needed - validation happens before API calls
			},
			expectError: true,
			errorMsg:    "at least one of loginpass or loginsshkey is required",
		},
		{
			name: "validation_missing_netconfig",
			job: &agent.RawJob{
				ID: "test-job-8",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:       1,
					Memory:    512,
					Disk:      10,
					Template:  "debian-13",
					NetConfig: nil, // Missing
					LoginUser: "testuser",
					LoginPass: "testpass123",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				// No mocks needed - validation happens before API calls
			},
			expectError: true,
			errorMsg:    "netconfig is required",
		},
		{
			name:         "validation_loginuser_with_sshkey",
			expectedVMID: 104,
			job: &agent.RawJob{
				ID: "test-job-9",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "debian-13",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser:   "testuser",
					LoginSSHKey: "ssh-rsa AAAAB3NzaC1yc2E...",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 104,
					Nodes:    []string{"node1"},
					Pools:    []string{"pool1"},
					Templates: []PveTemplate{
						{ID: 100, Name: "debian-13"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVMFromTemplate(testUserID, matchVmCreateFromTemplate(104, 100, "test-vm", "pool1", 1, 512, 10)).Return(nil).Once()
				mockClient.EXPECT().WaitForVMReady(mock.Anything, testUserID, 104, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
			},
			expectError: false,
		},
		{
			name:         "validation_loginuser_with_both_pass_and_sshkey",
			expectedVMID: 105,
			job: &agent.RawJob{
				ID: "test-job-10",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "debian-13",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.101",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser:   "testuser",
					LoginPass:   "testpass123",
					LoginSSHKey: "ssh-rsa AAAAB3NzaC1yc2E...",
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 105,
					Nodes:    []string{"node1"},
					Pools:    []string{"pool1"},
					Templates: []PveTemplate{
						{ID: 100, Name: "debian-13"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVMFromTemplate(testUserID, matchVmCreateFromTemplate(105, 100, "test-vm", "pool1", 1, 512, 10)).Return(nil).Once()
				mockClient.EXPECT().WaitForVMReady(mock.Anything, testUserID, 105, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
			},
			expectError: false,
		},
		{
			name: "no_templates_available",
			job: &agent.RawJob{
				ID: "test-job-11",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "debian-13",
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
					NextVMID:  106,
					Nodes:     []string{"node1"},
					Pools:     []string{"pool1"},
					Templates: []PveTemplate{}, // Empty templates
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
			},
			expectError: true,
			errorMsg:    "no templates available",
		},
		{
			name: "template_not_found",
			job: &agent.RawJob{
				ID: "test-job-12",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   512,
					Disk:     10,
					Template: "nonexistent-template",
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
					NextVMID: 107,
					Nodes:    []string{"node1"},
					Pools:    []string{"pool1"},
					Templates: []PveTemplate{
						{ID: 100, Name: "debian-13"},
						{ID: 101, Name: "ubuntu-24.04"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
			},
			expectError: true,
			errorMsg:    "template 'nonexistent-template' not found",
		},
		{
			name:         "successful_vm_creation_with_firewall_rules",
			expectedVMID: 108,
			job: &agent.RawJob{
				ID: "test-job-13",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm-firewall",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      2,
					Memory:   2048,
					Disk:     30,
					Template: "debian-13",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.110",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "admin",
					LoginPass: "securepass",
					FirewallRules: []FirewallRule{
						{
							Type:    "in",
							Proto:   "tcp",
							Action:  "ACCEPT",
							Dport:   "22",
							Comment: "Allow SSH",
						},
						{
							Type:    "in",
							Proto:   "tcp",
							Action:  "ACCEPT",
							Dport:   "80,443",
							Comment: "Allow HTTP/HTTPS",
						},
					},
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 108,
					Nodes:    []string{"node1", "node2"},
					Pools:    []string{"pool1"},
					Templates: []PveTemplate{
						{ID: 100, Name: "debian-13"},
						{ID: 101, Name: "ubuntu-24.04"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVMFromTemplate(testUserID, matchVmCreateFromTemplate(108, 100, "test-vm-firewall", "pool1", 2, 2048, 30)).Return(nil).Once()
				mockClient.EXPECT().WaitForVMReady(mock.Anything, testUserID, 108, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
				// Expect firewall rule calls
				mockClient.EXPECT().AddVMFirewallRule(testUserID, 108, mock.MatchedBy(func(rule *FirewallRule) bool {
					return rule.Type == "in" && rule.Proto == "tcp" && rule.Dport == "22" && rule.Comment == "Allow SSH"
				})).Return(nil).Once()
				mockClient.EXPECT().AddVMFirewallRule(testUserID, 108, mock.MatchedBy(func(rule *FirewallRule) bool {
					return rule.Type == "in" && rule.Proto == "tcp" && rule.Dport == "80,443" && rule.Comment == "Allow HTTP/HTTPS"
				})).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name:         "vm_creation_firewall_failure_does_not_block",
			expectedVMID: 109,
			job: &agent.RawJob{
				ID: "test-job-14",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID: "testuser",
					Name:       "test-vm-firewall-fail",
				}),
				Params: rawParamsPtr(VMProperties{
					CPU:      1,
					Memory:   1024,
					Disk:     20,
					Template: "ubuntu-24.04",
					NetConfig: &NetworkConfig{
						IPAddress: "10.0.1.111",
						Netmask:   24,
						Gateway:   "10.0.1.1",
					},
					LoginUser: "ubuntu",
					LoginPass: "ubuntu123",
					FirewallRules: []FirewallRule{
						{
							Type:    "in",
							Proto:   "tcp",
							Action:  "ACCEPT",
							Dport:   "8080",
							Comment: "App port",
						},
					},
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				userResources := &PveUserResources{
					NextVMID: 109,
					Nodes:    []string{"node1"},
					Pools:    []string{"pool1"},
					Templates: []PveTemplate{
						{ID: 100, Name: "debian-13"},
						{ID: 101, Name: "ubuntu-24.04"},
					},
				}
				mockClient.EXPECT().GetUserPools(testUserID).Return(userResources, nil).Once()
				mockClient.EXPECT().CreateVMFromTemplate(testUserID, matchVmCreateFromTemplate(109, 101, "test-vm-firewall-fail", "pool1", 1, 1024, 20)).Return(nil).Once()
				mockClient.EXPECT().WaitForVMReady(mock.Anything, testUserID, 109, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
				// Firewall rule fails but VM creation should still succeed
				mockClient.EXPECT().AddVMFirewallRule(testUserID, 109, mock.MatchedBy(func(rule *FirewallRule) bool {
					return rule.Type == "in" && rule.Proto == "tcp" && rule.Dport == "8080"
				})).Return(fmt.Errorf("firewall API error")).Once()
			},
			expectError: false, // VM creation succeeds even though firewall failed
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
				assert.Equal(t, tt.expectedVMID, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, fmt.Sprintf("%d", tt.expectedVMID), *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
			}
		})
	}
}

func TestVMAgent_UpdateVMHandler(t *testing.T) {
	tests := []struct {
		name         string
		job          *agent.RawJob
		setupMock    func(*MockProxmoxClient)
		expectError  bool
		errorMsg     string
		expectedVMID int
	}{
		{
			name:         "successful_vm_update",
			expectedVMID: 200,
			job: &agent.RawJob{
				ID: "test-job-1",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            agent.ServiceStatusStopped,
					AgentInstanceData: rawResourcesPtr(`{"vmid": 200}`),
				}),
				Params: rawUpdateParamsPtr(VMUpdateProperties{
					CPU:    intPtr(4),
					Memory: intPtr(2048),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM(testUserID, 200, &VmPatch{
					CPUs:   intPtr(4),
					Memory: intPtr(2048),
				}).Return(nil).Once()
			},
			expectError: false,
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
				Params: rawUpdateParamsPtr(VMUpdateProperties{
					CPU:    intPtr(2),
					Memory: intPtr(1024),
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
				Params: rawUpdateParamsPtr(VMUpdateProperties{
					CPU:    intPtr(8),
					Memory: intPtr(4096),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM(testUserID, 202, &VmPatch{
					CPUs:   intPtr(8),
					Memory: intPtr(4096),
				}).Return(errors.New("VM is locked")).Once()
			},
			expectError: true,
			errorMsg:    "failed to update VM",
		},
		{
			name:         "successful_update_with_firewall_rules",
			expectedVMID: 203,
			job: &agent.RawJob{
				ID: "test-job-5",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            agent.ServiceStatusStopped,
					AgentInstanceData: rawResourcesPtr(`{"vmid": 203}`),
				}),
				Params: rawUpdateParamsPtr(VMUpdateProperties{
					CPU:    intPtr(4),
					Memory: intPtr(2048),
					FirewallRules: firewallRulesPtr([]FirewallRule{
						{
							Type:    "in",
							Proto:   "tcp",
							Action:  "ACCEPT",
							Dport:   "443",
							Comment: "Allow HTTPS",
						},
						{
							Type:    "in",
							Proto:   "tcp",
							Action:  "ACCEPT",
							Dport:   "3306",
							Comment: "Allow MySQL",
						},
					}),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM(testUserID, 203, &VmPatch{
					CPUs:   intPtr(4),
					Memory: intPtr(2048),
				}).Return(nil).Once()
				// Expect GetVMFirewallRules for reconciliation
				mockClient.EXPECT().GetVMFirewallRules(testUserID, 203).Return([]FirewallRule{}, nil).Once()
				// Expect firewall rule additions
				mockClient.EXPECT().AddVMFirewallRule(testUserID, 203, mock.MatchedBy(func(rule *FirewallRule) bool {
					return rule.Type == "in" && rule.Proto == "tcp" && rule.Dport == "443" && rule.Comment == "Allow HTTPS"
				})).Return(nil).Once()
				mockClient.EXPECT().AddVMFirewallRule(testUserID, 203, mock.MatchedBy(func(rule *FirewallRule) bool {
					return rule.Type == "in" && rule.Proto == "tcp" && rule.Dport == "3306" && rule.Comment == "Allow MySQL"
				})).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name:         "update_firewall_failure_does_not_block",
			expectedVMID: 204,
			job: &agent.RawJob{
				ID: "test-job-6",
				Service: (*agent.Service[json.RawMessage, json.RawMessage])(&agent.RawService{
					ConsumerID:        "testuser",
					Name:              "test-vm",
					Status:            agent.ServiceStatusStopped,
					AgentInstanceData: rawResourcesPtr(`{"vmid": 204}`),
				}),
				Params: rawUpdateParamsPtr(VMUpdateProperties{
					CPU:    intPtr(2),
					Memory: intPtr(1024),
					FirewallRules: firewallRulesPtr([]FirewallRule{
						{
							Type:    "in",
							Proto:   "tcp",
							Action:  "ACCEPT",
							Dport:   "9000",
							Comment: "App port",
						},
					}),
				}),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().PatchVM(testUserID, 204, &VmPatch{
					CPUs:   intPtr(2),
					Memory: intPtr(1024),
				}).Return(nil).Once()
				// Firewall reconciliation fails but VM update should still succeed
				mockClient.EXPECT().GetVMFirewallRules(testUserID, 204).Return(nil, fmt.Errorf("firewall service unavailable")).Once()
			},
			expectError: false, // VM update succeeds even though firewall failed
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
			} else {
				require.NoError(t, err, "Expected update VM handler to succeed")
				require.NotNil(t, rawResponse, "Response should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceData, "Response agent instance data should not be nil")
				require.NotNil(t, rawResponse.AgentInstanceID, "Response agent instance ID should not be nil")

				// Parse the agent instance data to verify VMID
				var resources VMResources
				err := json.Unmarshal(*rawResponse.AgentInstanceData, &resources)
				require.NoError(t, err, "Should be able to unmarshal agent instance data")
				assert.Equal(t, tt.expectedVMID, resources.VMID, "Response VMID should match expected")
				assert.Equal(t, fmt.Sprintf("%d", tt.expectedVMID), *rawResponse.AgentInstanceID, "Agent instance ID should match VMID as string")
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
