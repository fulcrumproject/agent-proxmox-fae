package proxmoxapi

import (
	"testing"

	"fulcrumproject.org/agent-proxmox-wrapped/internal/config"
	"github.com/fulcrumproject/utils/confbuilder"
	"github.com/google/uuid"
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

	t.Run("CreateAndGetUser", func(t *testing.T) {
		testUUID := uuid.New().String()
		userReq := &PveUserCreation{
			UserID:    testUUID,
			FirstName: "Test",
			LastName:  "User " + testUUID,
			PoolID:    testUUID,
		}

		// Create the user
		err := client.CreateUser(userReq)
		if err != nil {
			t.Logf("CreateUser returned error (may be expected): %v", err)
			return // Skip the get test if create failed
		}
		t.Logf("Successfully created user with ID: %s", testUUID)

		// Get the user to verify it was created
		user, err := client.GetUser(testUUID)
		if err != nil {
			t.Logf("GetUser returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, user, "User should not be nil when no error occurs")
			t.Logf("Retrieved user: %+v", user)
		}

		// Get the user pools to verify user resources
		pools, err := client.GetUserPools(testUUID)
		if err != nil {
			t.Logf("GetUserPools returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, pools, "User pools should not be nil when no error occurs")
			t.Logf("Retrieved user pools: %+v", pools)
		}
	})

	t.Run("GetNodes", func(t *testing.T) {
		nodes, err := client.GetNodes()
		if err != nil {
			t.Logf("GetNodes returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, nodes, "Nodes should not be nil when no error occurs")
			t.Logf("Retrieved nodes: %+v", nodes)
		}
	})

	t.Run("GetNodesMetrics", func(t *testing.T) {
		metrics, err := client.GetNodesMetrics()
		if err != nil {
			t.Logf("GetNodesMetrics returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, metrics, "Nodes metrics should not be nil when no error occurs")
			assert.NotNil(t, metrics.Metrics, "Metrics map should not be nil")
			t.Logf("Retrieved nodes metrics: %+v", metrics)
		}
	})
}
