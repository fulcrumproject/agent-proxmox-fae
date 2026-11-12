package proxmoxapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the real HTTP client without touching a Proxmox cluster.
func TestClientHTTPContract(t *testing.T) {
	const username = "user-test"
	const createBody = `{"node":"node1","vmid":200,"templatevmid":100,"name":"test-vm","pool":"pool-test","cpus":2,"memory":1024,"disksize":20,"loginuser":"testuser","loginsshkey":"ssh-ed25519 test-key","net0_bridge":"vmbr","net0_ipaddress":"192.0.2.10","net0_netmask":24,"net0_gateway":"192.0.2.1","net0_nameserver":"192.0.2.53","net0_searchdomain":"example.test"}`
	var createReq vmagent.VmCreateFromTemplate
	require.NoError(t, json.Unmarshal([]byte(createBody), &createReq))
	pos := 0

	tests := []struct {
		name, method, path, user, body, response string
		call                                     func(*testing.T, *Client) error
	}{
		{"ping", "GET", "/ping", "", "", `{"fae":true,"proxmox":true}`, func(t *testing.T, c *Client) error {
			result, err := c.Ping()
			if assert.NoError(t, err) {
				assert.True(t, result.Fae)
				assert.True(t, result.Proxmox)
			}
			return err
		}},
		{"create user", "POST", "/pve/pveuser", "", `{"userid":"user-test","poolid":"pool-test"}`, "", func(t *testing.T, c *Client) error {
			return c.CreateUser(&vmagent.PveUserCreation{UserID: username, PoolID: "pool-test"})
		}},
		{"get user", "GET", "/pve/pveuser", username, "", `{"enabled":true,"hasApi":true,"status":true}`, func(t *testing.T, c *Client) error {
			_, err := c.GetUser(username)
			return err
		}},
		{"resources", "GET", "/pve/pveuser/resources", username, "", `{"nextvmid":200,"nodes":["node1"],"pools":["pool-test"],"templates":[{"id":100,"name":"debian-template","node":"node1"}],"vnet":false,"vdc":false}`, func(t *testing.T, c *Client) error {
			result, err := c.GetUserPools(username)
			if assert.NoError(t, err) {
				assert.Equal(t, 200, result.NextVMID)
				assert.Equal(t, []vmagent.PveTemplate{{ID: 100, Name: "debian-template"}}, result.Templates)
			}
			return err
		}},
		{"nodes", "GET", "/pve/nodes", "", "", `["node1"]`, func(t *testing.T, c *Client) error {
			_, err := c.GetNodes()
			return err
		}},
		{"node metrics", "GET", "/pve/nodes/metrics", "", "", `{"metrics":{"node1":{"cpu":{"cur":1,"max":4,"pct":25}}}}`, func(t *testing.T, c *Client) error {
			_, err := c.GetNodesMetrics()
			return err
		}},
		{"create VM", "POST", "/pve/vms/create/from-template", username, createBody, "", func(t *testing.T, c *Client) error {
			return c.CreateVMFromTemplate(username, &createReq)
		}},
		{"get VM", "GET", "/pve/vms/200", username, "", `{"vmid":200,"status":"stopped","lock":false,"maxcpu":2,"maxmem":1073741824,"nets":{},"disks":[],"qemuReady":false}`, func(t *testing.T, c *Client) error {
			result, err := c.GetVM(username, 200)
			if assert.NoError(t, err) {
				assert.Equal(t, 200, result.VMID)
				assert.Equal(t, "stopped", result.Status)
				assert.False(t, result.Lock)
				assert.Equal(t, float64(2), result.MaxCPU)
			}
			return err
		}},
		{"patch VM", "PATCH", "/pve/vms/200", username, `{"cpus":4}`, "", func(t *testing.T, c *Client) error {
			var req vmagent.VmPatch
			require.NoError(t, json.Unmarshal([]byte(`{"cpus":4}`), &req))
			return c.PatchVM(username, 200, &req)
		}},
		{"delete VM", "DELETE", "/pve/vms/200", username, "", "", func(t *testing.T, c *Client) error { return c.DeleteVM(username, 200) }},
		{"start VM", "POST", "/pve/vms/200/start", username, "", "", func(t *testing.T, c *Client) error { return c.StartVM(username, 200) }},
		{"stop VM", "POST", "/pve/vms/200/stop", username, "", "", func(t *testing.T, c *Client) error { return c.StopVM(username, 200) }},
		{"VNC", "POST", "/pve/vms/200/vnc", username, `{"source_ip":"192.0.2.5","web":false}`, `{"host":"console.example.test"}`, func(t *testing.T, c *Client) error {
			_, err := c.OpenVNC(username, 200, &VncEnable{SourceIP: "192.0.2.5"})
			return err
		}},
		{"get firewall", "GET", "/pve/vms/200/firewall", username, "", `[{"type":"in","action":"ACCEPT","proto":"tcp","dport":"22","pos":0,"digest":"abc123"}]`, func(t *testing.T, c *Client) error {
			result, err := c.GetVMFirewallRules(username, 200)
			if assert.NoError(t, err) && assert.Len(t, result, 1) {
				assert.Equal(t, &pos, result[0].Pos)
				assert.Equal(t, "abc123", result[0].Digest)
			}
			return err
		}},
		{"add firewall", "POST", "/pve/vms/200/firewall", username, `{"type":"in","action":"ACCEPT","proto":"tcp","dport":"22"}`, "", func(t *testing.T, c *Client) error {
			return c.AddVMFirewallRule(username, 200, &vmagent.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22", Pos: &pos, Digest: "abc123"})
		}},
		{"delete firewall", "DELETE", "/pve/vms/200/firewall/0", username, "", "", func(t *testing.T, c *Client) error { return c.DeleteVMFirewallRule(username, 200, 0) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, tt.method, r.Method)
				assert.Equal(t, tt.path, r.URL.RequestURI())
				assert.Equal(t, "test-token", r.Header.Get("Authorization"))
				assert.Equal(t, tt.user, r.Header.Get("PVE-User"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				if tt.body == "" {
					assert.Empty(t, body)
				} else {
					assert.JSONEq(t, tt.body, string(body))
				}
				if tt.response == "" {
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, tt.response)
				}
			}))
			defer server.Close()
			client := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, APIToken: "test-token", Timeout: 2})
			defer client.restyClient.Close()
			require.NoError(t, tt.call(t, client))
			assert.Equal(t, 1, requests)
		})
	}
}

func TestClientHTTPRejectsErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := NewClient(&config.ProxmoxAPIConfig{APIURLs: []string{server.URL}, Timeout: 2})
			defer client.restyClient.Close()
			_, err := client.GetVM("user-test", 200)
			require.Error(t, err)
			var httpErr *HTTPError
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, status, httpErr.StatusCode)
			assert.Equal(t, status == http.StatusNotFound, waitForVMDeleted(t, client, "user-test", 200, time.Second))
		})
	}
}
