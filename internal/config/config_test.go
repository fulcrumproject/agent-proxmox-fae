package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxmoxProvisioningTimeouts(t *testing.T) {
	defaults := DefaultProxmoxAPIConfig()
	assert.Equal(t, 30, defaults.Timeout)
	assert.Equal(t, 10*time.Minute, defaults.CreationTimeout())
	assert.Equal(t, 30*time.Minute, defaults.ReadinessTimeout())

	var oldRemoteConfig ProxmoxAPIConfig
	require.NoError(t, json.Unmarshal([]byte(`{"apiUrls":["https://fae.example"],"apiToken":"test","timeout":30}`), &oldRemoteConfig))
	assert.Equal(t, 10*time.Minute, oldRemoteConfig.CreationTimeout())
	assert.Equal(t, 30*time.Minute, oldRemoteConfig.ReadinessTimeout())

	var configured ProxmoxAPIConfig
	require.NoError(t, json.Unmarshal([]byte(`{"createTimeout":120,"readyTimeout":900}`), &configured))
	assert.Equal(t, 2*time.Minute, configured.CreationTimeout())
	assert.Equal(t, 15*time.Minute, configured.ReadinessTimeout())
}
