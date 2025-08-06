package handlers

import (
	"context"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// MetricsReporter collects and returns metrics
func MetricsReporter() agent.MetricsReporter[ServiceProperties] {
	return func(ctx context.Context) ([]agent.MetricEntry, error) {
		var metrics []agent.MetricEntry
		return metrics, nil
	}
}
