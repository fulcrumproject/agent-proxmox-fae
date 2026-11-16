package vmagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateVMFAEPayload(t *testing.T) {
	client := NewMockProxmoxClient(t)
	client.EXPECT().GetUserPools(testUserID).Return(&PveUserResources{
		NextVMID: 200, Nodes: []string{"node1"}, Pools: []string{testPoolID},
		Templates: []PveTemplate{{ID: 100, Name: "debian-template"}},
	}, nil).Once()
	client.EXPECT().CreateVMFromTemplate(testUserID, mock.Anything).Run(func(_ string, req *VmCreateFromTemplate) {
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.JSONEq(t, `{"node":"node1","vmid":200,"templatevmid":100,"name":"test-vm","pool":"pool-testuser","cpus":2,"memory":1024,"disksize":20,"loginuser":"testuser","loginsshkey":"ssh-ed25519 test-key","net0_bridge":"vmbr","net0_ipaddress":"192.0.2.10","net0_netmask":24,"net0_gateway":"192.0.2.1","net0_nameserver":"192.0.2.53","net0_searchdomain":"example.test"}`, string(body))
	}).Return(nil).Once()
	client.EXPECT().WaitForVMReady(mock.Anything, testUserID, 200, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
	vmAgent := &VMAgent{proxmoxClient: client}
	response, err := agent.JobHandlerWrapper(vmAgent.CreateVMHandler())(context.Background(), &agent.RawJob{
		ID:      "test-job",
		Service: &agent.Service[json.RawMessage, json.RawMessage]{ConsumerID: testConsumerID, Name: "test-vm"},
		Params: rawParamsPtr(VMProperties{
			CPU: 2, Memory: 1024, Disk: 20, Template: "debian-template",
			LoginUser: "testuser", LoginSSHKey: "ssh-ed25519 test-key",
			NetConfig:  &NetworkConfig{IPAddress: "192.0.2.10", Netmask: 24, Gateway: "192.0.2.1"},
			Nameserver: "192.0.2.53", SearchDomain: "example.test",
		}),
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, "200", *response.AgentInstanceID)
}

func TestUpdateVMFAEPartialPayload(t *testing.T) {
	for _, tt := range []struct {
		name   string
		params VMUpdateProperties
		body   string
	}{
		{"cpu only", VMUpdateProperties{CPU: intPtr(4)}, `{"cpus":4}`},
		{"memory only", VMUpdateProperties{Memory: intPtr(2048)}, `{"memory":2048}`},
		{"disk only", VMUpdateProperties{Disk: intPtr(30)}, `{"disksize":30}`},
		{"explicit zero is not omission", VMUpdateProperties{CPU: intPtr(0)}, `{"cpus":0}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockProxmoxClient(t)
			client.EXPECT().PatchVM(testUserID, 200, mock.Anything).Run(func(_ string, _ int, req *VmPatch) {
				body, err := json.Marshal(req)
				require.NoError(t, err)
				assert.JSONEq(t, tt.body, string(body))
			}).Return(nil).Once()
			vmAgent := &VMAgent{proxmoxClient: client}
			_, err := agent.JobHandlerWrapper(vmAgent.UpdateVMHandler())(context.Background(), &agent.RawJob{
				ID:      "test-job",
				Service: &agent.Service[json.RawMessage, json.RawMessage]{ConsumerID: testConsumerID, Name: "test-vm", AgentInstanceData: rawResourcesPtr(`{"vmid":200}`)},
				Params:  rawUpdateParamsPtr(tt.params),
			})
			require.NoError(t, err)
		})
	}
}
