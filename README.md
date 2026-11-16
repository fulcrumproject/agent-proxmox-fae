# Fulcrum Proxmox Agent

A Proxmox Virtual Environment (PVE) agent for the Fulcrum Core platform

## Overview

The Fulcrum Proxmox Agent integrates Proxmox Virtual Environment with the Fulcrum Core platform, enabling centralized management of virtual machines running on Proxmox clusters. This agent acts as a bridge between Fulcrum Core and Proxmox PVE, translating Fulcrum operations into Proxmox API calls.

This agent implements the complete Fulcrum agent protocol, including:
- Agent registration and authentication with Fulcrum Core
- VM lifecycle management (create, start, stop, delete) via Proxmox API
- Job queue processing for Proxmox operations
- Metrics collection from Proxmox VMs and hosts
- Real-time status synchronization between Proxmox and Fulcrum

## Installation

### Prerequisites

- Go 1.24 or higher
- Access to a running Fulcrum Core API
- Proxmox Virtual Environment cluster with API access
- Agent type and provider registered in Fulcrum Core
- Network connectivity between the agent and both Fulcrum Core and Proxmox PVE

### Building

```bash
# Build the agent
go build -o fulcrum-proxmox-agent ./cmd/agent
```

## Configuration

The Proxmox agent can be configured using a combination of a configuration file and environment variables.

### Configuration File

Create a JSON configuration file (e.g., `config.json`):

```json
{
  "agentToken": "YOUR_AGENT_TOKEN",
  "fulcrumApiUrl": "http://localhost:3000",
  "jobPollInterval": "5s",
  "metricReportInterval": "30s"
}
```

### Default Configuration Values

The agent uses the following default values if not specified:

| Parameter              | Default Value           | Description                 |
| ---------------------- | ----------------------- | --------------------------- |
| `agentToken`           | (empty)                 | Must be provided            |
| `fulcrumApiUrl`        | "http://localhost:3000" | URL of the Fulcrum Core API |
| `jobPollInterval`      | 5s                      | How often to poll for jobs  |
| `metricReportInterval` | 30s                     | How often to report metrics |

### Environment Variables

Configuration can also be provided or overridden using environment variables. The agent automatically prepends `PROXMOX_AGENT_` to the field names defined in the Config struct:

- `PROXMOX_AGENT_FULCRUM_API_TOKEN`: Secret token of the agent
- `PROXMOX_AGENT_FULCRUM_API_URL`: URL of the Fulcrum Core API
- `PROXMOX_AGENT_JOB_POLL_INTERVAL`: How often to poll for jobs
- `PROXMOX_AGENT_METRIC_REPORT_INTERVAL`: How often to report metrics

### Proxmox API Configuration

Proxmox API connection parameters (API token, URL, timeout, etc.) are automatically provided by Fulcrum Core during the agent connection phase. These parameters do not need to be configured in the agent configuration file or environment variables.

## Virtual Machine Lifecycle

The agent manages VM lifecycle operations through the Proxmox API with the following state machine:

```mermaid
statusDiagram-v2
    [*] --> CREATED: CreateVM
    CREATED --> STARTED: StartVM
    STARTED --> STOPPED: StopVM
    STOPPED --> STARTED: StartVM
    STOPPED --> DELETED: DeleteVM
    DELETED --> [*]
```

### VM States

| State   | Description                      | Available Operations        |
| ------- | -------------------------------- | --------------------------- |
| CREATED | Initial state after VM creation  | StartVM                     |
| STARTED | VM is running on Proxmox         | StopVM                      |
| STOPPED | VM exists but is not running     | StartVM, DeleteVM, UpdateVM |
| DELETED | VM has been removed from Proxmox | (None)                      |

Each state transition is executed through Proxmox API calls and may take time depending on the VM configuration and Proxmox cluster performance.

## Usage

### Running the Agent

```bash
# Run with default configuration (requires PROXMOX_AGENT_FULCRUM_API_TOKEN to be set)
./fulcrum-proxmox-agent

# Run with a configuration file
./fulcrum-proxmox-agent -config config.json
```

### Stopping the Agent

The agent handles SIGINT and SIGTERM signals for graceful shutdown. Simply press `Ctrl+C` to stop it cleanly.

## Metrics Generated

The Proxmox agent collects and reports the following metrics from Proxmox VMs:

### Resource Utilization Metrics

- `vm.cpu.usage`: CPU usage percentage from Proxmox VM statistics
- `vm.memory.usage`: Memory usage percentage from Proxmox VM statistics

All metrics are collected directly from the Proxmox API and reflect real VM resource consumption. The agent reports these metrics at the configured interval for all managed VMs.

## Job Processing

The Proxmox agent can process the following job types from Fulcrum Core:

- `ServiceCreate`: Creates a new VM on the Proxmox cluster
- `ServiceUpdate`: Updates a VM configuration when stopped (hot-update is not supported)
- `ServiceStart`: Starts a VM on the Proxmox cluster
- `ServiceStop`: Stops a VM on the Proxmox cluster
- `ServiceDelete`: Removes a VM from the Proxmox cluster

Jobs are processed according to the state machine rules and Proxmox API capabilities. If a job requests an invalid state transition or encounters a Proxmox API error, the agent will report the error back to Fulcrum Core.

## Proxmox Integration

The agent integrates with Proxmox Virtual Environment through the Opiquad Enhanced Proxmox API wrapper, which provides additional features and simplified access to Proxmox functionality.

### FAE API Compatibility

The client targets the FAE 0.17 API, with the reference schema in
`internal/proxmoxapi/openapi.json` (version 0.17.5.822). Configure the FAE
**base URL**, for example `https://fae-test.example:5297`, without `/pve`
or `/docs`. The client uses `/pve/...` routes and POST for start/stop;
the old `/fae/v1/...` routes are not supported.

Fulcrum service properties remain unchanged: `cpu`, `memory`, `disk`,
`netconfig`, DNS, login and firewall properties. The agent translates them
to FAE's `cpus` and `net0_*` fields and omits fields not supplied in partial
updates. It uses `net0_bridge: "vmbr"` for the infrastructure network.
The operator must supply IP/prefix/gateway values appropriate to that
bridge through the existing Fulcrum network pool. Tagged allocations can include
`netconfig.vlantag` (1–4094), forwarded as `net0_vlantag`; omit it for an
untagged network. The agent does not provision vnets, VDCs, DHCP or routing.

Creation has two separate time budgets in the remote `proxmoxAPI` config:
`createTimeout` for the create HTTP request (default 600 seconds), and
`readyTimeout` for provisioning (default 1800 seconds). Zero or omitted values
use those defaults, including older remote configurations. The existing
`timeout` still controls ordinary HTTP requests (default 30 seconds).

A stopped/unlocked clone is not necessarily initialized: the create job also
requires `qemuReady=true` before completing or applying firewall rules.
The provisioning wait respects job cancellation. **The original create-to-Stopped
contract is retained**: the agent never issues an automatic StopVM to conceal a
ready VM remaining running. Such an environment needs an explicit lifecycle
decision before the full integration test can pass.

The existing `user-<consumer-id>` / `pool-<consumer-id>` identity mapping is
preserved. The FAE API token, scoped `PVE-User` header and guest login
credentials remain separate.

### API Communication
- Uses the **Opiquad Enhanced Proxmox API wrapper** for streamlined VM operations
- Built on top of Proxmox VE REST API with enhanced functionality
- Supports authentication via API tokens or username/password
- Handles API rate limiting and connection pooling
- Provides additional endpoints for improved VM management and monitoring

### VM Management
- Creates VMs from templates or ISO images
- Manages VM configurations (CPU, memory, disk, network)
- Supports both QEMU/KVM and LXC containers

### Monitoring
- Polls Proxmox API for real-time VM metrics
- Tracks resource usage and performance statistics
- Reports VM state changes back to Fulcrum Core

## Development

### Hot Reloading

The project includes a configuration file for Air (`.air.toml`), which provides hot reloading during development:

```bash
# Install Air
go install github.com/cosmtrek/air@latest

# Run with Air for development
air
```

This will automatically rebuild and restart the agent when source files change.

## Troubleshooting

### Agent Won't Register

Make sure:
- Fulcrum Core API is running and accessible
- The provided agent token exists in Fulcrum Core
- Check connection errors in the agent logs

### Proxmox Connection Issues

Check:
- Proxmox VE API is accessible from the agent
- API credentials are valid and have sufficient permissions
- Network connectivity between agent and Proxmox cluster
- Proxmox API rate limits are not being exceeded

### No Metrics Appearing

Check:
- The metric type names are registered in Fulcrum Core
- The agent is successfully registered
- Proxmox API is returning valid metric data
- Verify the metrics reporting interval

### VM Operations Failing

If VM operations are failing:
1. Check the agent logs for Proxmox API errors
2. Verify VM exists on the Proxmox cluster
3. Ensure the Proxmox user has sufficient permissions
4. Review the state transition rules in the VM lifecycle section
5. Check Proxmox cluster resources and storage availability

## Development and Testing

The Proxmox agent includes comprehensive unit tests and integration tests:

### Unit Tests

```bash
# Run all tests
go test ./...

# Run specific tests
go test -v ./internal/vmagent
```

### Integration Tests

Integration tests contact a real FAE **test environment**. Normal
`go test ./...` runs the offline HTTP-contract and agent-handler tests,
skipping live tests.

#### Setup

If you do not already have a local configuration file:

```bash
cp .env_proxmoxapi.test.example .env_proxmoxapi.test
chmod 600 .env_proxmoxapi.test
```

Configure a single FAE base URL and token in `.env_proxmoxapi.test`:

```dotenv
PROXMOX_AGENT_PROXMOX_API_URLS=https://fae-test.example:5297
PROXMOX_AGENT_PROXMOX_API_TOKEN="YOUR_FAE_TOKEN"
PROXMOX_AGENT_PROXMOX_API_INSECURE_SKIP_TLS=false
PROXMOX_AGENT_PROXMOX_API_TIMEOUT=30
PROXMOX_AGENT_PROXMOX_API_CREATE_TIMEOUT=600
PROXMOX_AGENT_PROXMOX_API_READY_TIMEOUT=1800
PROXMOX_AGENT_PROXMOX_API_DEBUG=false
```

`URLS` is plural; the timeout is an integer number of seconds. The loader
finds this file in the repository root even when Go runs tests from the
package directory. The file is ignored by Git; never commit credentials.
Use a hostname matching the certificate; TLS verification bypass is only
appropriate for an explicitly trusted test endpoint. Live tests disable
HTTP debug logging and require a single endpoint.

#### Read-only checks

```bash
INTEGRATION_TESTS=1 go test -count=1 -v ./internal/proxmoxapi \
  -run '^(TestClientPing|TestClientGetNodes|TestClientGetNodesMetrics)$'
```

These check connectivity, both health flags, nodes and node metrics.
Authentication/API errors fail the tests rather than merely being logged.

#### VM lifecycle

Add the following environment-specific fixtures to the same local file:

```dotenv
PROXMOX_AGENT_TEST_TEMPLATE=YOUR_LINUX_CLOUD_INIT_TEMPLATE
PROXMOX_AGENT_TEST_IPADDRESS=YOUR_RESERVED_TEST_IPV4_ADDRESS
PROXMOX_AGENT_TEST_NETMASK=24
PROXMOX_AGENT_TEST_GATEWAY=YOUR_TEST_GATEWAY
# Optional (set the VLAN for tagged networks):
PROXMOX_AGENT_TEST_VLAN_TAG=
PROXMOX_AGENT_TEST_NAMESERVER=
PROXMOX_AGENT_TEST_SEARCHDOMAIN=
PROXMOX_AGENT_TEST_VNC_SOURCE_IP=
```

Reserve an unused address on the infrastructure bridge (`vmbr`). Do not
copy an address from a production VM or assume DHCP is available. Choose
a Linux cloud-init template with a base disk no larger than 20 GiB.
The test provisions 4 CPUs and 4096 MiB RAM to allow guest initialization and
updates. Missing/invalid required fixtures fail before user/VM creation.
A fresh Ed25519 SSH key is generated for each run; the shared repository key is
not used on public test VMs.

```bash
INTEGRATION_TESTS=1 go test -count=1 -v -timeout=60m ./internal/proxmoxapi \
  -run '^TestClientVMLifecycle$'
```

The test creates a uniquely named user/pool and VM, then verifies:
- User/resource/template access and VM creation/readiness.
- Separate CPU, memory and disk updates, preserving unrelated fields.
- Firewall rule creation, retrieval and deletion.
- VM start, status/metric reads and stop.
- VM deletion, confirmed by HTTP 404, not an arbitrary API failure.

On success, the test deletes the VM and its temporary SSH key. **On failure,
it does not stop or delete the VM**: the VM identity and protected local SSH-key
path are logged for operator inspection. A timed-out create may still finish
on FAE; do not assume absence from an immediate 404 or blindly rerun with the
same IP. Explicit cleanup is required after investigating a failed run.
**FAE exposes no user/pool deletion endpoint:** their IDs are logged for operator cleanup.
`TestClientCreateAndGetUser` and `TestClientGetTemplates` also create users,
so running the entire integration suite is not read-only.

The optional VNC subtest is explicitly skipped unless a source IP is
configured. The lifecycle test does not verify SSH reachability or the full
Fulcrum job-dispatch path; it exercises the real FAE client. Handler mapping
and metrics reporting are additionally covered by offline tests.
