package vmagent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVMAgent_HealthHandler(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockProxmoxClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_ping",
			setupMock: func(mockClient *MockProxmoxClient) {
				pingResp := &PingResponse{
					Fae:     true,
					Proxmox: true,
				}
				mockClient.EXPECT().Ping().Return(pingResp, nil).Once()
			},
			expectError: false,
		},
		{
			name: "ping_fails",
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().Ping().Return(nil, errors.New("connection failed")).Once()
			},
			expectError: true,
			errorMsg:    "connection failed",
		},
		{
			name: "ping_returns_nil_response",
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().Ping().Return(nil, nil).Once()
			},
			expectError: false,
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

			// Get the health handler
			healthHandler := vmAgent.HealthHandler()

			// Verify that HealthHandler returns a function
			require.NotNil(t, healthHandler, "HealthHandler should return a non-nil function")

			// Execute the health handler
			ctx := context.Background()
			err := healthHandler(ctx)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected health handler to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
			} else {
				require.NoError(t, err, "Expected health handler to succeed")
			}
		})
	}
}
