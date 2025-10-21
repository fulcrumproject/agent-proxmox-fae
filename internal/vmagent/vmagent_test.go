package vmagent

import (
	"context"
	"testing"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVMAgent_Success(t *testing.T) {
	// Create mock fulcrum client
	mockFulcrumClient := NewMockFulcrumClient(t)

	// Create proxmox client factory
	proxmoxClientFactory := func(config *config.ProxmoxAPIConfig) ProxmoxClient {
		mockClient := NewMockProxmoxClient(t)
		return mockClient
	}

	// Create VMAgent
	vmAgent := NewVMAgent(proxmoxClientFactory, mockFulcrumClient)

	// Verify agent was created successfully
	require.NotNil(t, vmAgent, "VMAgent should not be nil")
	require.NotNil(t, vmAgent.stdAgent, "stdAgent should not be nil")
	assert.NotNil(t, vmAgent.proxmoxClientFactory, "proxmoxClientFactory should not be nil")

	// Verify all handlers are configured by checking they return non-nil functions
	assert.NotNil(t, vmAgent.HealthHandler(), "HealthHandler should be configured")
	assert.NotNil(t, vmAgent.CreateVMHandler(), "CreateVMHandler should be configured")
	assert.NotNil(t, vmAgent.UpdateVMHandler(), "UpdateVMHandler should be configured")
	assert.NotNil(t, vmAgent.StartVMHandler(), "StartVMHandler should be configured")
	assert.NotNil(t, vmAgent.StopVMHandler(), "StopVMHandler should be configured")
	assert.NotNil(t, vmAgent.DeleteVMHandler(), "DeleteVMHandler should be configured")
	assert.NotNil(t, vmAgent.MetricsReporter(), "MetricsReporter should be configured")
}

func TestVMAgent_ConnectHandler_Success(t *testing.T) {
	// Create a mock proxmox client that will be returned by the factory
	mockProxmoxClient := NewMockProxmoxClient(t)

	// Setup successful ping response
	pingResp := &PingResponse{
		Fae:     true,
		Proxmox: true,
	}
	mockProxmoxClient.EXPECT().Ping().Return(pingResp, nil).Once()

	// Create a proxmox client factory that returns our mock
	proxmoxClientFactory := func(config *config.ProxmoxAPIConfig) ProxmoxClient {
		return mockProxmoxClient
	}

	// Create VMAgent with the factory
	vmAgent := &VMAgent{
		proxmoxClientFactory: proxmoxClientFactory,
	}

	// Create test agent info
	agentInfo := &agent.AgentInfo[RemoteConfig]{
		Config: &RemoteConfig{
			ProxmoxAPI: &config.ProxmoxAPIConfig{
				APIURL:   "https://proxmox.example.com:8006",
				APIToken: "test-token-12345",
			},
		},
	}

	// Execute the connect handler
	ctx := context.Background()
	err := vmAgent.ConnectHandler(ctx, agentInfo)

	// Verify success
	require.NoError(t, err, "Connect handler should succeed")
	assert.NotNil(t, vmAgent.proxmoxClient, "Proxmox client should be set after successful connection")
}
