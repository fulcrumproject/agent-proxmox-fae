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
- `vm.disk.usage`: Disk usage and I/O statistics from Proxmox
- `vm.network.usage`: Network traffic statistics from Proxmox

All metrics are collected directly from the Proxmox API and reflect real VM resource consumption. The agent reports these metrics at the configured interval for all managed VMs.

## Job Processing

The Proxmox agent can process the following job types from Fulcrum Core:

- `ServiceCreate`: Creates a new VM on the Proxmox cluster
- `ServiceColdUpdate`: Updates a VM configuration when stopped
- `ServiceHotUpdate`: Updates a VM configuration while running (if supported)
- `ServiceStart`: Starts a VM on the Proxmox cluster
- `ServiceStop`: Stops a VM on the Proxmox cluster
- `ServiceDelete`: Removes a VM from the Proxmox cluster

Jobs are processed according to the state machine rules and Proxmox API capabilities. If a job requests an invalid state transition or encounters a Proxmox API error, the agent will report the error back to Fulcrum Core.

## Proxmox Integration

The agent integrates with Proxmox Virtual Environment through the following mechanisms:

### API Communication
- Uses Proxmox VE REST API for all VM operations
- Supports authentication via API tokens or username/password
- Handles API rate limiting and connection pooling

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

The Proxmox agent includes comprehensive unit tests:

```bash
# Run all tests
go test ./...

# Run specific tests
go test -v ./internal/handlers
```
