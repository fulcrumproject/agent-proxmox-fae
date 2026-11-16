package vmagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateVMFinalizesStoppedOnlyAfterReadiness(t *testing.T) {
	stopFailure := errors.New("stop rejected")
	for _, tt := range []struct {
		name, state                         string
		readyErr, stopErr, waitErr, wantErr error
		cancelWhenReady                     bool
	}{
		{name: "already stopped needs no stop", state: "stopped"},
		{name: "running is stopped after readiness", state: "running"},
		{name: "not ready must not stop", state: "running", readyErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
		{name: "stop failure prevents completion", state: "running", stopErr: stopFailure, wantErr: stopFailure},
		{name: "stop timeout prevents completion", state: "running", waitErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
		{name: "cancelled job must not stop", state: "running", cancelWhenReady: true, wantErr: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockProxmoxClient(t)
			expectProvisioningResources(client)
			created := client.EXPECT().CreateVMFromTemplate(testUserID, mock.Anything).Return(nil).Once()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var readyVM *Vm
			if tt.readyErr == nil {
				readyVM = &Vm{VMID: 200, Status: tt.state, QemuReady: true}
			}
			ready := client.EXPECT().WaitForVMReady(ctx, testUserID, 200, config.DefaultReadyTimeout).
				Run(func(context.Context, string, int, time.Duration) {
					if tt.cancelWhenReady {
						cancel()
					}
				}).
				Return(readyVM, tt.readyErr).Once()
			ready.NotBefore(created)
			finalStep := ready
			if tt.readyErr == nil && !tt.cancelWhenReady && tt.state == "running" {
				stop := client.EXPECT().StopVM(testUserID, 200).Return(tt.stopErr).Once()
				stop.NotBefore(ready)
				if tt.stopErr == nil {
					stopped := client.EXPECT().WaitForVMState(testUserID, 200, "stopped", time.Minute).Return(tt.waitErr).Once()
					stopped.NotBefore(stop)
					finalStep = stopped
				}
			}
			job := provisioningJob(t, "")
			var props VMProperties
			require.NoError(t, json.Unmarshal(*job.Params, &props))
			props.FirewallRules = []FirewallRule{{Type: "in", Proto: "tcp", Action: "ACCEPT", Dport: "22"}}
			job.Params = rawParamsPtr(props)
			if tt.wantErr == nil {
				firewall := client.EXPECT().AddVMFirewallRule(testUserID, 200, mock.Anything).Return(nil).Once()
				firewall.NotBefore(finalStep)
			}
			v := &VMAgent{proxmoxClient: client}
			result, err := agent.JobHandlerWrapper(v.CreateVMHandler())(ctx, job)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, "200", *result.AgentInstanceID)
			}
		})
	}
}
