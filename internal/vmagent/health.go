package vmagent

import (
	"context"
	"log/slog"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// healthHandler handles health events and updates VM resources
func (v *VMAgent) HealthHandler() agent.HealthHandler {
	return func(ctx context.Context) error {
		// Ping proxmox API
		ping, err := v.proxmoxClient.Ping()
		if err != nil {
			return err
		}
		slog.Info("Proxmox API ping", "ping", ping)

		return nil
	}
}
