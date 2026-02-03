package estimator

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestNewGroupHistory(t *testing.T) {
	gh := NewGroupHistory("test-group", "default", 100)
	assert.Equal(t, "test-group", gh.GroupName)
	assert.Equal(t, "default", gh.Namespace)
	assert.Equal(t, 0, len(gh.History))
	assert.Equal(t, 100, gh.maxSize)
}

func TestGroupHistory_AddUsage(t *testing.T) {
	gh := NewGroupHistory("test", "default", 3)

	gh.AddUsage(100, 2048, 1)
	gh.AddUsage(150, 3072, 1)
	gh.AddUsage(200, 4096, 2)

	assert.Equal(t, 3, len(gh.History))
	assert.Equal(t, 200.0, gh.History[2].CPU)

	// Test FIFO eviction
	gh.AddUsage(250, 5120, 2)
	assert.Equal(t, 3, len(gh.History))
	assert.Equal(t, 150.0, gh.History[0].CPU) // First entry evicted
}

func TestGroupHistory_GetAverage(t *testing.T) {
	gh := NewGroupHistory("test", "default", 10)

	gh.AddUsage(100, 2000, 1)
	gh.AddUsage(200, 4000, 2)
	gh.AddUsage(300, 6000, 3)

	avg := gh.GetAverage()
	assert.Equal(t, 200.0, avg.CPU)
	assert.Equal(t, 4000.0, avg.Memory)
	assert.Equal(t, 2.0, avg.GPU)
}

func TestGroupHistory_GetPeak(t *testing.T) {
	gh := NewGroupHistory("test", "default", 10)

	gh.AddUsage(100, 2000, 1)
	gh.AddUsage(300, 8000, 3)
	gh.AddUsage(200, 4000, 2)

	peak := gh.GetPeak()
	assert.Equal(t, 300.0, peak.CPU)
	assert.Equal(t, 8000.0, peak.Memory)
	assert.Equal(t, 3.0, peak.GPU)
}

func TestGroupHistory_EmptyHistory(t *testing.T) {
	gh := NewGroupHistory("test", "default", 10)

	avg := gh.GetAverage()
	assert.Equal(t, 0.0, avg.CPU)

	peak := gh.GetPeak()
	assert.Equal(t, 0.0, peak.CPU)
}

func TestNewEstimator(t *testing.T) {
	est := NewEstimator(100, slog.Default())
	assert.NotNil(t, est)
	assert.NotNil(t, est.logger)
	assert.Equal(t, 100, est.maxSize)
	assert.Equal(t, 0, len(est.histories))
}

func TestEstimator_RecordUsage(t *testing.T) {
	est := NewEstimator(10, slog.Default())

	est.RecordUsage("default", "group1", 100, 2048, 1)
	est.RecordUsage("default", "group1", 150, 3072, 1)

	history, exists := est.GetHistory("default", "group1")
	require.True(t, exists)
	assert.Equal(t, 2, len(history.History))
}

func TestEstimator_EstimateResources(t *testing.T) {
	est := NewEstimator(10, slog.Default())

	// Record multiple usage datapoints (in cores)
	est.RecordUsage("default", "ml-training", 1000, 4096, 2)
	est.RecordUsage("default", "ml-training", 1500, 6144, 2)
	est.RecordUsage("default", "ml-training", 2000, 8192, 4)

	resources, err := est.EstimateResources("default", "ml-training")
	require.NoError(t, err)
	require.NotNil(t, resources)

	// Verify resources exist
	cpu, exists := resources[corev1.ResourceCPU]
	assert.True(t, exists)
	assert.True(t, cpu.Value() > 0)

	memory, exists := resources[corev1.ResourceMemory]
	assert.True(t, exists)
	assert.True(t, memory.Value() > 0)

	gpu, exists := resources["nvidia.com/gpu"]
	assert.True(t, exists)
	assert.True(t, gpu.Value() > 0)
}

func TestEstimator_EstimateResources_NoHistory(t *testing.T) {
	est := NewEstimator(10, slog.Default())

	_, err := est.EstimateResources("default", "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no history found")
}

func TestEstimator_EstimateResources_WeightedAverage(t *testing.T) {
	est := NewEstimator(10, slog.Default())

	// Create predictable pattern (values in cores)
	// avg CPU: (1000+1000+2000)/3 = 1333.33
	// peak CPU: 2000
	// weighted: 70% * 1333.33 + 30% * 2000 = 933.33 + 600 = 1533.33 cores
	est.RecordUsage("default", "test", 1000, 2000, 1)
	est.RecordUsage("default", "test", 1000, 2000, 1)
	est.RecordUsage("default", "test", 2000, 4000, 2)

	resources, err := est.EstimateResources("default", "test")
	require.NoError(t, err)

	// CPU is stored in milli-cores, so 1533.33 cores = ~1533330 milli-cores
	cpu := resources[corev1.ResourceCPU]
	cpuMilliCores := float64(cpu.MilliValue())
	assert.InDelta(t, 1533330, cpuMilliCores, 100) // Allow small variance due to rounding
}

func TestEstimator_CleanOldHistory(t *testing.T) {
	est := NewEstimator(10, slog.Default())

	// Add recent usage
	est.RecordUsage("default", "group1", 100, 2048, 1)

	// Add old usage by manipulating timestamp
	history, _ := est.GetHistory("default", "group1")
	history.mu.Lock()
	history.History[0].Timestamp = time.Now().Add(-48 * time.Hour)
	history.mu.Unlock()

	// Clean histories older than 24 hours
	removed := est.CleanOldHistory(24 * time.Hour)
	assert.Equal(t, 1, removed)

	_, exists := est.GetHistory("default", "group1")
	assert.False(t, exists)
}

func TestEstimator_ConcurrentAccess(t *testing.T) {
	est := NewEstimator(100, slog.Default())

	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				est.RecordUsage("default", "concurrent-test", float64(100*id), 2048, 1)
			}
			done <- true
		}(i)
	}

	// Wait for all writers
	for i := 0; i < 10; i++ {
		<-done
	}

	history, exists := est.GetHistory("default", "concurrent-test")
	require.True(t, exists)
	assert.True(t, len(history.History) > 0)
}
