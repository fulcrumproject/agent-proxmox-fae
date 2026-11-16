package vmagent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func provisioningJob(t *testing.T, vlan string) *agent.RawJob {
	t.Helper()
	return &agent.RawJob{
		ID:      "provisioning-test",
		Service: &agent.Service[json.RawMessage, json.RawMessage]{ConsumerID: testConsumerID, Name: "test-vm"},
		Params:  rawResourcesPtr(`{"cpu":4,"memory":4096,"disk":20,"template":"debian-template","loginuser":"fulcrum","loginsshkey":"ssh-ed25519 test-key","netconfig":{"ipaddress":"192.0.2.10","netmask":24,"gateway":"192.0.2.1"` + vlan + `}}`),
	}
}

func expectProvisioningResources(client *MockProxmoxClient) {
	client.EXPECT().GetUserPools(testUserID).Return(&PveUserResources{
		NextVMID: 200, Nodes: []string{"node1"}, Pools: []string{testPoolID},
		Templates: []PveTemplate{{ID: 100, Name: "debian-template"}},
	}, nil).Once()
}

func TestCreateVMPropagatesVLAN(t *testing.T) {
	client := NewMockProxmoxClient(t)
	expectProvisioningResources(client)
	client.EXPECT().CreateVMFromTemplate(testUserID, mock.Anything).Run(func(_ string, req *VmCreateFromTemplate) {
		data, err := json.Marshal(req)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(data, &payload))
		assert.Equal(t, float64(6), payload["net0_vlantag"])
	}).Return(nil).Once()
	client.EXPECT().WaitForVMReady(mock.Anything, testUserID, 200, mock.Anything).Return(&Vm{Status: "stopped", QemuReady: true}, nil).Once()
	v := &VMAgent{proxmoxClient: client}
	_, err := agent.JobHandlerWrapper(v.CreateVMHandler())(context.Background(), provisioningJob(t, `,"vlantag":6`))
	require.NoError(t, err)
}

func TestCreateVMDoesNotCompleteBeforeProvisioning(t *testing.T) {
	for _, timeout := range []time.Duration{0, 5 * time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			client := NewMockProxmoxClient(t)
			expectProvisioningResources(client)
			client.EXPECT().CreateVMFromTemplate(testUserID, mock.Anything).Return(nil).Once()
			expected := timeout
			if expected == 0 {
				expected = config.DefaultReadyTimeout
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client.EXPECT().WaitForVMReady(ctx, testUserID, 200, expected).Return(nil, context.DeadlineExceeded).Once()
			v := &VMAgent{proxmoxClient: client, readyTimeout: timeout}
			response, err := agent.JobHandlerWrapper(v.CreateVMHandler())(ctx, provisioningJob(t, ""))
			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Nil(t, response)
		})
	}
}

func TestCreateVMRejectsInvalidVLANBeforeSideEffects(t *testing.T) {
	for _, tag := range []int{-1, 0, 4095} {
		t.Run(fmt.Sprint(tag), func(t *testing.T) {
			client := NewMockProxmoxClient(t)
			v := &VMAgent{proxmoxClient: client}
			response, err := agent.JobHandlerWrapper(v.CreateVMHandler())(context.Background(), provisioningJob(t, fmt.Sprintf(`,"vlantag":%d`, tag)))
			require.ErrorContains(t, err, "vlantag")
			assert.Nil(t, response)
		})
	}
}
