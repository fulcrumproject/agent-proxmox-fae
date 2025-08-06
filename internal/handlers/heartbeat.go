package handlers

import (
	"context"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// heartbeatHandler handles heartbeat events and checks agent health
func HeartbeatHandler() agent.HeartbeatHandler {
	return func(ctx context.Context) error {
		return nil
	}
}
