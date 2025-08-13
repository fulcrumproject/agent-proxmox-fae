package vmagent

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// VMServiceProperties represents the properties of a VM service
type VMServiceProperties struct {
	CPU    int    `json:"cpu"`
	Memory int    `json:"memory"`
	Disk   int    `json:"disk"`
	Image  string `json:"image"`
}

// JobResources represents the resources in a job response
type JobResources struct {
	VMID int `json:"vmid"`
}

// extractUserAndVMID extracts the username from job service info
func extractUserAndVMID(service *agent.Service) (string, int, error) {
	if service == nil {
		return "", 0, fmt.Errorf("job service is nil")
	}

	if service.Resources == nil {
		return "", 0, fmt.Errorf("job service resources are nil")
	}

	vmId, ok := service.Resources["vmid"]
	if !ok {
		return "", 0, fmt.Errorf("vmid not found in job service resources")
	}

	vmIdInt, ok := vmId.(int)
	if !ok {
		return "", 0, fmt.Errorf("vmid is not an integer")
	}

	return service.ConsumerID, vmIdInt, nil
}

// newJobResponse creates a successful job response
func (v *VMAgent) newJobResponse(vmid int) *agent.JobResponse[JobResources] {
	vmidStr := strconv.Itoa(vmid)
	return &agent.JobResponse[JobResources]{
		Resources: &JobResources{
			VMID: vmid,
		},
		ExternalID: &vmidStr,
	}
}

// CreateVMHandler handles VM creation jobs
func (v *VMAgent) CreateVMHandler() agent.JobHandler[VMServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[VMServiceProperties]) (*agent.JobResponse[JobResources], error) {
		slog.Info("Processing VM creation job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user from job
		username := job.Service.ConsumerID

		// Get user resources to determine available nodes and pools
		userResources, err := v.proxmoxClient.GetUserPools(username)
		if err != nil {
			slog.Error("Failed to get user resources", "user", username, "error", err)
			return nil, fmt.Errorf("failed to get user resources: %w", err)
		}

		// Get next available VM ID
		nextID := userResources.NextID

		// Select a random node from the list of available nodes
		if len(userResources.Nodes) == 0 {
			slog.Error("No nodes available for user", "user", username)
			return nil, fmt.Errorf("no nodes available for user %s", username)
		}
		nodeName := userResources.Nodes[rand.Intn(len(userResources.Nodes))]

		// Select the first pool from the list of available pools
		if len(userResources.Pools) == 0 {
			slog.Error("No pools available for user", "user", username)
			return nil, fmt.Errorf("no pools available for user %s", username)
		}
		poolName := userResources.Pools[0]

		// Create VM configuration
		vmConfig := &VmCreation{
			Node:    nodeName,
			VMID:    nextID,
			Name:    job.Service.Name,
			Pool:    poolName,
			Sockets: 1,
			Cores:   job.Params.CPU,
			Memory:  job.Params.Memory,
			SCSI0:   job.Params.Disk,
			IDE2:    job.Params.Image, // ISO image or template
		}

		// Create the VM
		err = v.proxmoxClient.CreateVM(username, vmConfig)
		if err != nil {
			slog.Error("Failed to create VM", "vmid", nextID, "error", err)
			return nil, fmt.Errorf("failed to create VM: %w", err)
		}

		slog.Info("Successfully created VM", "vmid", nextID, "name", job.Service.Name, "user", username)
		return v.newJobResponse(nextID), nil
	}
}

// UpdateVMHandler handles VM update jobs
func (v *VMAgent) UpdateVMHandler() agent.JobHandler[VMServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[VMServiceProperties]) (*agent.JobResponse[JobResources], error) {
		slog.Info("Processing VM update job", "jobID", job.ID, "service", job.Service.Name)

		// If the VM is not stopped, return not supported
		if job.Service.Status != agent.ServiceStatusStopped {
			return nil, &agent.UnsupportedJobError{Msg: fmt.Sprintf("cannot update a VM in status %s", job.Service.Status)}
		}

		// Extract user and VMID from job
		username, vmid, err := extractUserAndVMID(job.Service)
		if err != nil {
			slog.Error("Failed to extract VMID from job", "error", err)
			return nil, fmt.Errorf("failed to extract VMID: %w", err)
		}

		// Create patch configuration
		patchConfig := &VmPatch{
			Cores:  job.Params.CPU,
			Memory: job.Params.Memory,
		}

		// Update the VM
		err = v.proxmoxClient.PatchVM(username, vmid, patchConfig)
		if err != nil {
			slog.Error("Failed to update VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to update VM: %w", err)
		}

		slog.Info("Successfully updated VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}

// StartVMHandler handles VM start jobs
func (v *VMAgent) StartVMHandler() agent.JobHandler[VMServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[VMServiceProperties]) (*agent.JobResponse[JobResources], error) {
		slog.Info("Processing VM start job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, vmid, err := extractUserAndVMID(job.Service)
		if err != nil {
			slog.Error("Failed to extract VMID from job", "error", err)
			return nil, fmt.Errorf("failed to extract VMID: %w", err)
		}

		// Start the VM
		err = v.proxmoxClient.StartVM(username, vmid)
		if err != nil {
			slog.Error("Failed to start VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to start VM: %w", err)
		}

		slog.Info("Successfully started VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}

// StopVMHandler handles VM stop jobs
func (v *VMAgent) StopVMHandler() agent.JobHandler[VMServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[VMServiceProperties]) (*agent.JobResponse[JobResources], error) {
		slog.Info("Processing VM stop job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, vmid, err := extractUserAndVMID(job.Service)
		if err != nil {
			slog.Error("Failed to extract VMID from job", "error", err)
			return nil, fmt.Errorf("failed to extract VMID: %w", err)
		}

		// Stop the VM
		err = v.proxmoxClient.StopVM(username, vmid)
		if err != nil {
			slog.Error("Failed to stop VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to stop VM: %w", err)
		}

		slog.Info("Successfully stopped VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}

// DeleteVMHandler handles VM delete jobs
func (v *VMAgent) DeleteVMHandler() agent.JobHandler[VMServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[VMServiceProperties]) (*agent.JobResponse[JobResources], error) {
		slog.Info("Processing VM delete job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, vmid, err := extractUserAndVMID(job.Service)
		if err != nil {
			slog.Error("Failed to extract VMID from job", "error", err)
			return nil, fmt.Errorf("failed to extract VMID: %w", err)
		}

		// Delete the VM
		err = v.proxmoxClient.DeleteVM(username, vmid)
		if err != nil {
			slog.Error("Failed to delete VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to delete VM: %w", err)
		}

		slog.Info("Successfully deleted VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}
