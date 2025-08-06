package handlers

import (
	"context"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// ServiceProperties represents the properties of a VM service
type ServiceProperties struct{}

// JobResources represents the resources in a job response
type JobResources struct{}

// CreateVMHandler handles VM creation jobs
func CreateVMHandler() agent.JobHandler[ServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[ServiceProperties]) (*agent.JobResponse[JobResources], error) {
		return &agent.JobResponse[JobResources]{}, nil
	}
}

// UpdateVMHandler handles VM update jobs
func UpdateVMHandler() agent.JobHandler[ServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[ServiceProperties]) (*agent.JobResponse[JobResources], error) {
		return &agent.JobResponse[JobResources]{}, nil
	}
}

// StartVMHandler handles VM start jobs
func StartVMHandler() agent.JobHandler[ServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[ServiceProperties]) (*agent.JobResponse[JobResources], error) {
		return &agent.JobResponse[JobResources]{}, nil
	}
}

// StopVMHandler handles VM stop jobs
func StopVMHandler() agent.JobHandler[ServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[ServiceProperties]) (*agent.JobResponse[JobResources], error) {
		return &agent.JobResponse[JobResources]{}, nil
	}
}

// DeleteVMHandler handles VM delete jobs
func DeleteVMHandler() agent.JobHandler[ServiceProperties, JobResources] {
	return func(ctx context.Context, job *agent.Job[ServiceProperties]) (*agent.JobResponse[JobResources], error) {
		return &agent.JobResponse[JobResources]{}, nil
	}
}
