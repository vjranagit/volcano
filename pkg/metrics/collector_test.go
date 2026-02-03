package metrics

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCollector(t *testing.T) {
	collector := NewCollector(slog.Default())
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.logger)
}

func TestGroupMetrics(t *testing.T) {
	collector := NewCollector(slog.Default())

	collector.SetGroupsTotal("ready", 5)
	collector.SetGroupsTotal("pending", 3)
	collector.ObserveGroupReadyDuration(10.5)
	collector.IncGroupTimeouts()
	collector.SetGroupPods("test-group", "default", "Running", 4)

	// Just verify methods work without errors
	assert.NotNil(t, collector)
}

func TestQuotaMetrics(t *testing.T) {
	collector := NewCollector(slog.Default())

	collector.SetQuotaAllocated("default", "cpu", 100.0)
	collector.SetQuotaAvailable("default", "cpu", 50.0)
	collector.SetQuotaBorrowed("default", "memory", 2048.0)
	collector.IncQuotaPreemptions()

	assert.NotNil(t, collector)
}

func TestEventMetrics(t *testing.T) {
	collector := NewCollector(slog.Default())

	collector.IncEventsPublished("GroupReady")
	collector.IncEventsDropped("PodAdded")
	collector.SetEventBusBufferSize(150.0)

	assert.NotNil(t, collector)
}

func TestSchedulerMetrics(t *testing.T) {
	collector := NewCollector(slog.Default())

	collector.IncSchedulingAttempts("success")
	collector.IncSchedulingAttempts("failure")
	collector.ObserveSchedulingLatency(0.025)

	assert.NotNil(t, collector)
}
