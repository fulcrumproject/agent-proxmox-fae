package proxmoxapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForVMReadyRequiresProvisioningAndOperationalState(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		ready      bool
	}{
		{"initial stopped clone", `{"status":"stopped","lock":false,"qemuReady":false}`, false},
		{"missing readiness flag", `{"status":"stopped","lock":false}`, false},
		{"ready but locked", `{"status":"stopped","lock":true,"qemuReady":true}`, false},
		{"ready and running", `{"status":"running","lock":false,"qemuReady":true}`, true},
		{"paused is not operational", `{"status":"paused","lock":false,"qemuReady":true}`, false},
		{"ready and stopped", `{"status":"stopped","lock":false,"qemuReady":true}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/pve/vms/200", r.URL.Path)
				assert.Equal(t, "user-test", r.Header.Get("PVE-User"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 2})
			defer c.restyClient.Close()
			v, err := c.WaitForVMReady(context.Background(), "user-test", 200, 100*time.Millisecond)
			if tt.ready {
				require.NoError(t, err)
				require.NotNil(t, v)
				assert.True(t, v.QemuReady)
				assert.False(t, v.Lock)
				assert.Contains(t, []string{"running", "stopped"}, v.Status)
			} else {
				assert.Nil(t, v)
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
		})
	}
}

func TestWaitForVMReadyPollsThroughInitialization(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		states := []string{
			`{"status":"stopped","lock":false,"qemuReady":false}`,
			`{"status":"running","lock":false,"qemuReady":false}`,
			`{"status":"running","lock":false,"qemuReady":true}`,
		}
		n := int(calls.Add(1)) - 1
		if n >= len(states) {
			n = len(states) - 1
		}
		fmt.Fprint(w, states[n])
	}))
	defer server.Close()
	c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 2})
	defer c.restyClient.Close()
	v, err := c.WaitForVMReady(context.Background(), "user-test", 200, 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "running", v.Status)
	assert.Equal(t, int32(3), calls.Load())
}

func TestWaitForVMReadyCancellation(t *testing.T) {
	t.Run("already cancelled makes no request", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
		defer server.Close()
		c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 30})
		defer c.restyClient.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		v, err := c.WaitForVMReady(ctx, "user-test", 200, time.Minute)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, v)
		assert.Zero(t, calls.Load())
	})
	t.Run("deadline cancels in-flight HTTP", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 30})
		defer c.restyClient.Close()
		start := time.Now()
		v, err := c.WaitForVMReady(context.Background(), "user-test", 200, 100*time.Millisecond)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, v)
		assert.Less(t, time.Since(start), 2*time.Second)
	})
}

func TestWaitForVMReadyHTTPErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 2})
			defer c.restyClient.Close()
			v, err := c.WaitForVMReady(context.Background(), "user-test", 200, 100*time.Millisecond)
			assert.Nil(t, v)
			if status == 404 || status == 429 || status >= 500 {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				var apiErr *HTTPError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, status, apiErr.StatusCode)
			}
		})
	}
}

func TestGetVMDoesNotReuseReadinessFromFailedEndpoint(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"status":"stopped","qemuReady":true,"vmid":"invalid"}`)
			return
		}
		fmt.Fprint(w, `{"status":"stopped","lock":false,"vmid":200}`)
	}))
	defer server.Close()
	// Identical URLs make the request order deterministic despite endpoint shuffling.
	c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL, server.URL}, Timeout: 2})
	defer c.restyClient.Close()
	v, err := c.GetVM("user-test", 200)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
	assert.False(t, v.QemuReady, "An omitted readiness flag must not retain a partially decoded value from a failed attempt")
}

func TestCreateRequestUsesSeparateTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(1200 * time.Millisecond):
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"vmid": 200, "status": "stopped", "qemuReady": true})
	}))
	defer server.Close()
	c := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 1, CreateTimeout: 3})
	defer c.restyClient.Close()
	require.NoError(t, c.CreateVMFromTemplate("user-test", &vmagent.VmCreateFromTemplate{VMID: 200}))
	_, err := c.GetVM("user-test", 200)
	require.ErrorIs(t, err, context.DeadlineExceeded, "The normal request timeout must remain short")
}
