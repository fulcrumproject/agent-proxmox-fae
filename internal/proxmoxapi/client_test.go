package proxmoxapi

import (
	"testing"

	"fulcrumproject.org/agent-proxmox-wrapped/internal/config"
	"github.com/fulcrumproject/utils/confbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient(t *testing.T) {
	cfg, err := confbuilder.New(config.DefaultConfig()).
		EnvPrefix(config.EnvPrefix).
		EnvFiles(".env").
		Build()
	require.NoError(t, err, "Failed to get config")

	client := NewClient(&cfg.ProxmoxAPI)
	require.NotNil(t, client, "Client should be created successfully")

	t.Run("Ping", func(t *testing.T) {
		_, err := client.Ping()
		assert.NoError(t, err, "Ping should succeed")
	})
}
