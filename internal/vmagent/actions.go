package vmagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// extractUserAndVMID extracts the username from job service info (for raw services)
func extractUserAndVMID(service *agent.RawService) (string, int, error) {
	if service == nil {
		return "", 0, fmt.Errorf("job service is nil")
	}

	if service.AgentInstanceData == nil {
		return "", 0, fmt.Errorf("job service agent instance data are nil")
	}

	var resources VMResources
	if err := json.Unmarshal(*service.AgentInstanceData, &resources); err != nil {
		return "", 0, fmt.Errorf("failed to unmarshal service agent instance data: %w", err)
	}

	// Validate that VMID was provided and is not zero
	if resources.VMID == 0 {
		return "", 0, fmt.Errorf("vmid is missing or zero in service agent instance data")
	}

	return service.ConsumerID, resources.VMID, nil
}

// newJobResponse creates a successful job response
func (v *VMAgent) newJobResponse(vmid int) *agent.JobResponse[VMResources] {
	vmidStr := strconv.Itoa(vmid)
	return &agent.JobResponse[VMResources]{
		AgentInstanceData: &VMResources{
			VMID: vmid,
		},
		AgentInstanceID: &vmidStr,
	}
}

// CreateVMHandler handles VM creation jobs
func (v *VMAgent) CreateVMHandler() agent.JobHandler[VMProperties, VMProperties, VMResources] {
	return func(ctx context.Context, job *agent.Job[VMProperties, VMProperties, VMResources]) (*agent.JobResponse[VMResources], error) {
		slog.Info("Processing VM creation job", "jobID", job.ID, "service", job.Service.Name)

		// Validate required fields
		if job.Params.LoginUser == "" {
			return nil, fmt.Errorf("loginuser is required")
		}
		if job.Params.LoginPass == "" && job.Params.LoginSSHKey == "" {
			return nil, fmt.Errorf("at least one of loginpass or loginsshkey is required")
		}
		if job.Params.NetConfig == nil {
			return nil, fmt.Errorf("netconfig is required")
		}

		// Extract user from job and create prefixed IDs
		username, poolID := MakeUserIDs(job.Service.ConsumerID)

		// Get user resources to determine available nodes and pools
		userResources, err := v.proxmoxClient.GetUserPools(username)
		if err != nil {
			slog.Info("User not found, attempting to create user", "user", username)

			// Create user if it doesn't exist
			userReq := &PveUserCreation{
				UserID:        username,
				PoolID:        poolID,
				ResetIfExists: false,
			}

			createErr := v.proxmoxClient.CreateUser(userReq)
			if createErr != nil {
				slog.Error("Failed to create user", "user", username, "error", createErr)
				return nil, fmt.Errorf("failed to create user: %w", createErr)
			}

			slog.Info("Successfully created user", "user", username)

			// Retry getting user resources after creation
			userResources, err = v.proxmoxClient.GetUserPools(username)
			if err != nil {
				slog.Error("Failed to get user resources after creation", "user", username, "error", err)
				return nil, fmt.Errorf("failed to get user resources after creation: %w", err)
			}
		}

		// Get next available VM ID
		nextID := userResources.NextVMID

		// Select a random node from the list of available nodes
		if len(userResources.Nodes) == 0 {
			slog.Error("No nodes available for user", "user", username)
			return nil, fmt.Errorf("no nodes available for user %s", username)
		}
		nodeName := userResources.Nodes[rand.Intn(len(userResources.Nodes))]

		// Generate a unique name for the VM
		vmName := job.Service.Name

		// Select the first pool from the list of available pools
		if len(userResources.Pools) == 0 {
			slog.Error("No pools available for user", "user", username)
			return nil, fmt.Errorf("no pools available for user %s", username)
		}
		poolName := userResources.Pools[0]

		// Validate and select template
		if len(userResources.Templates) == 0 {
			slog.Error("No templates available for user", "user", username)
			return nil, fmt.Errorf("no templates available for user %s", username)
		}

		// Find template by name matching user-specified template
		var selectedTemplate *PveTemplate
		for _, tmpl := range userResources.Templates {
			if tmpl.Name == job.Params.Template {
				selectedTemplate = &tmpl
				break
			}
		}
		if selectedTemplate == nil {
			slog.Error("Template not found", "template", job.Params.Template, "user", username)
			return nil, fmt.Errorf("template '%s' not found in available templates", job.Params.Template)
		}

		// Create VM configuration from template
		vmReq := &VmCreateFromTemplate{
			Node:         nodeName,
			VMID:         nextID,
			TemplateVMID: selectedTemplate.ID,
			Name:         vmName,
			Pool:         poolName,
			Sockets:      1,
			Cores:        job.Params.CPU,
			Memory:       job.Params.Memory,
			DiskSize:     job.Params.Disk,
			LoginUser:    job.Params.LoginUser,
			LoginPass:    job.Params.LoginPass,
			LoginSSHKey:  job.Params.LoginSSHKey,
			IPAddress:    job.Params.NetConfig.IPAddress,
			Netmask:      job.Params.NetConfig.Netmask,
			Gateway:      job.Params.NetConfig.Gateway,
			Nameserver:   job.Params.Nameserver,
			SearchDomain: job.Params.SearchDomain,
		}

		// Create the VM from template
		err = v.proxmoxClient.CreateVMFromTemplate(username, vmReq)
		if err != nil {
			slog.Error("Failed to create VM", "vmid", nextID, "error", err)
			return nil, fmt.Errorf("failed to create VM: %w", err)
		}

		slog.Info("Successfully created VM", "vmid", nextID, "name", job.Service.Name, "user", username)

		// Apply firewall rules if present (errors are logged but don't fail VM creation)
		if len(job.Params.FirewallRules) > 0 {
			slog.Info("Applying firewall rules", "vmid", nextID, "ruleCount", len(job.Params.FirewallRules))
			for i, rule := range job.Params.FirewallRules {
				err := v.proxmoxClient.AddVMFirewallRule(username, nextID, &rule)
				if err != nil {
					slog.Error("Failed to add firewall rule", "vmid", nextID, "ruleIndex", i, "rule", rule, "error", err)
					// Continue to next rule - don't fail VM creation
					continue
				}
				slog.Info("Successfully added firewall rule", "vmid", nextID, "ruleIndex", i, "comment", rule.Comment)
			}
		}

		return v.newJobResponse(nextID), nil
	}
}

// UpdateVMHandler handles VM update jobs
func (v *VMAgent) UpdateVMHandler() agent.JobHandler[VMUpdateProperties, VMUpdateProperties, VMResources] {
	return func(ctx context.Context, job *agent.Job[VMUpdateProperties, VMUpdateProperties, VMResources]) (*agent.JobResponse[VMResources], error) {
		slog.Info("Processing VM update job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, _ := MakeUserIDs(job.Service.ConsumerID)
		vmid := job.Service.AgentInstanceData.VMID

		// Validate that VMID was provided and is not zero
		if vmid == 0 {
			slog.Error("VMID is missing or zero in service resources")
			return nil, fmt.Errorf("vmid is missing or zero in service resources")
		}

		// Build patch configuration (only update provided fields)
		patchNeeded := false
		patchConfig := &VmPatch{}

		if job.Params.CPU != nil {
			patchConfig.Cores = *job.Params.CPU
			patchNeeded = true
			slog.Info("Updating CPU", "vmid", vmid, "cores", *job.Params.CPU)
		}

		if job.Params.Memory != nil {
			patchConfig.Memory = *job.Params.Memory
			patchNeeded = true
			slog.Info("Updating Memory", "vmid", vmid, "memory", *job.Params.Memory)
		}

		// Apply VM patches if any fields were provided
		if patchNeeded {
			err := v.proxmoxClient.PatchVM(username, vmid, patchConfig)
			if err != nil {
				slog.Error("Failed to update VM", "vmid", vmid, "error", err)
				return nil, fmt.Errorf("failed to update VM: %w", err)
			}
			slog.Info("Successfully updated VM", "vmid", vmid, "user", username)
		} else {
			slog.Info("No VM configuration changes requested", "vmid", vmid)
		}

		// Reconcile firewall rules if provided
		if job.Params.FirewallRules != nil {
			err := v.reconcileFirewallRules(username, vmid, *job.Params.FirewallRules)
			if err != nil {
				slog.Error("Failed to reconcile firewall rules", "vmid", vmid, "error", err)
				// Log error but don't fail the update - firewall is best-effort
			}
		}

		return v.newJobResponse(vmid), nil
	}
}

// reconcileFirewallRules reconciles the desired firewall rules with the current state
// It compares current rules with desired rules and adds/removes as needed
func (v *VMAgent) reconcileFirewallRules(username string, vmid int, desiredRules []FirewallRule) error {
	slog.Info("Reconciling firewall rules", "vmid", vmid, "desiredCount", len(desiredRules))

	// Get current rules from Proxmox
	currentRules, err := v.proxmoxClient.GetVMFirewallRules(username, vmid)
	if err != nil {
		return fmt.Errorf("failed to get current firewall rules: %w", err)
	}

	slog.Info("Current firewall rules fetched", "vmid", vmid, "currentCount", len(currentRules))

	// Build comparison maps (by rule signature, ignoring comment)
	currentMap := make(map[string]*FirewallRule)
	for i := range currentRules {
		sig := firewallRuleSignature(&currentRules[i])
		currentMap[sig] = &currentRules[i]
	}

	desiredMap := make(map[string]*FirewallRule)
	for i := range desiredRules {
		sig := firewallRuleSignature(&desiredRules[i])
		desiredMap[sig] = &desiredRules[i]
	}

	// Note: We cannot reliably delete rules by position because:
	// 1. The API's GetVMFirewallRules may not return position (Pos)
	// 2. Positions shift after each deletion
	// 3. The proxmoxapi.FirewallRule has Pos but vmagent.FirewallRule doesn't
	//
	// Strategy: Just add new rules (idempotent on the proxmox side hopefully)
	// For true reconciliation, we'd need to clear all rules first or use positions

	addCount := 0
	// Add rules that don't exist in current state
	for sig, rule := range desiredMap {
		if _, exists := currentMap[sig]; !exists {
			err := v.proxmoxClient.AddVMFirewallRule(username, vmid, rule)
			if err != nil {
				slog.Error("Failed to add firewall rule", "vmid", vmid, "rule", rule, "error", err)
				// Continue - best effort
			} else {
				slog.Info("Added firewall rule", "vmid", vmid, "comment", rule.Comment)
				addCount++
			}
		}
	}

	// Note: Deletion is skipped because we don't have reliable position information
	// To implement deletion, we would need:
	// 1. proxmoxapi.FirewallRule to vmagent.FirewallRule conversion to preserve Pos
	// 2. Delete rules in reverse order (highest pos first) to avoid position shifting
	// OR: Add a "clear all rules" API endpoint and rebuild from scratch

	slog.Info("Firewall reconciliation complete", "vmid", vmid, "added", addCount)
	return nil
}

// firewallRuleSignature creates a unique signature for a firewall rule
// Used for comparison (ignores comment field)
func firewallRuleSignature(rule *FirewallRule) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s:%s",
		rule.Type, rule.Proto, rule.Action,
		rule.Source, rule.Sport, rule.Dest, rule.Dport)
}

// StartVMHandler handles VM start jobs
func (v *VMAgent) StartVMHandler() agent.JobHandler[VMProperties, VMProperties, VMResources] {
	return func(ctx context.Context, job *agent.Job[VMProperties, VMProperties, VMResources]) (*agent.JobResponse[VMResources], error) {
		slog.Info("Processing VM start job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, _ := MakeUserIDs(job.Service.ConsumerID)
		vmid := job.Service.AgentInstanceData.VMID

		// Validate that VMID was provided and is not zero
		if vmid == 0 {
			slog.Error("VMID is missing or zero in service agent instance data")
			return nil, fmt.Errorf("vmid is missing or zero in service agent instance data")
		}

		// Start the VM
		err := v.proxmoxClient.StartVM(username, vmid)
		if err != nil {
			slog.Error("Failed to start VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to start VM: %w", err)
		}

		slog.Info("Successfully started VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}

// StopVMHandler handles VM stop jobs
func (v *VMAgent) StopVMHandler() agent.JobHandler[VMProperties, VMProperties, VMResources] {
	return func(ctx context.Context, job *agent.Job[VMProperties, VMProperties, VMResources]) (*agent.JobResponse[VMResources], error) {
		slog.Info("Processing VM stop job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, _ := MakeUserIDs(job.Service.ConsumerID)
		vmid := job.Service.AgentInstanceData.VMID

		// Validate that VMID was provided and is not zero
		if vmid == 0 {
			slog.Error("VMID is missing or zero in service agent instance data")
			return nil, fmt.Errorf("vmid is missing or zero in service agent instance data")
		}

		// Stop the VM
		err := v.proxmoxClient.StopVM(username, vmid)
		if err != nil {
			slog.Error("Failed to stop VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to stop VM: %w", err)
		}

		slog.Info("Successfully stopped VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}

// DeleteVMHandler handles VM delete jobs
func (v *VMAgent) DeleteVMHandler() agent.JobHandler[VMProperties, VMProperties, VMResources] {
	return func(ctx context.Context, job *agent.Job[VMProperties, VMProperties, VMResources]) (*agent.JobResponse[VMResources], error) {
		slog.Info("Processing VM delete job", "jobID", job.ID, "service", job.Service.Name)

		// Extract user and VMID from job
		username, _ := MakeUserIDs(job.Service.ConsumerID)
		vmid := job.Service.AgentInstanceData.VMID

		// Validate that VMID was provided and is not zero
		if vmid == 0 {
			slog.Error("VMID is missing or zero in service agent instance data")
			return nil, fmt.Errorf("vmid is missing or zero in service agent instance data")
		}

		// Delete the VM
		err := v.proxmoxClient.DeleteVM(username, vmid)
		if err != nil {
			slog.Error("Failed to delete VM", "vmid", vmid, "error", err)
			return nil, fmt.Errorf("failed to delete VM: %w", err)
		}

		slog.Info("Successfully deleted VM", "vmid", vmid, "user", username)
		return v.newJobResponse(vmid), nil
	}
}
