package estimator

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ResourceUsage tracks resource usage over time.
type ResourceUsage struct {
	Timestamp time.Time
	CPU       float64
	Memory    float64
	GPU       float64
}

// GroupHistory maintains historical resource usage for a job group.
type GroupHistory struct {
	GroupName string
	Namespace string
	History   []ResourceUsage
	maxSize   int
	mu        sync.RWMutex
}

// NewGroupHistory creates a new group history tracker.
func NewGroupHistory(groupName, namespace string, maxSize int) *GroupHistory {
	return &GroupHistory{
		GroupName: groupName,
		Namespace: namespace,
		History:   make([]ResourceUsage, 0, maxSize),
		maxSize:   maxSize,
	}
}

// AddUsage records a new resource usage datapoint.
func (gh *GroupHistory) AddUsage(cpu, memory, gpu float64) {
	gh.mu.Lock()
	defer gh.mu.Unlock()

	usage := ResourceUsage{
		Timestamp: time.Now(),
		CPU:       cpu,
		Memory:    memory,
		GPU:       gpu,
	}

	gh.History = append(gh.History, usage)

	// Keep only maxSize entries (FIFO)
	if len(gh.History) > gh.maxSize {
		gh.History = gh.History[1:]
	}
}

// GetAverage returns average resource usage.
func (gh *GroupHistory) GetAverage() ResourceUsage {
	gh.mu.RLock()
	defer gh.mu.RUnlock()

	if len(gh.History) == 0 {
		return ResourceUsage{}
	}

	var totalCPU, totalMem, totalGPU float64
	for _, usage := range gh.History {
		totalCPU += usage.CPU
		totalMem += usage.Memory
		totalGPU += usage.GPU
	}

	count := float64(len(gh.History))
	return ResourceUsage{
		CPU:    totalCPU / count,
		Memory: totalMem / count,
		GPU:    totalGPU / count,
	}
}

// GetPeak returns peak resource usage.
func (gh *GroupHistory) GetPeak() ResourceUsage {
	gh.mu.RLock()
	defer gh.mu.RUnlock()

	if len(gh.History) == 0 {
		return ResourceUsage{}
	}

	peak := gh.History[0]
	for _, usage := range gh.History {
		if usage.CPU > peak.CPU {
			peak.CPU = usage.CPU
		}
		if usage.Memory > peak.Memory {
			peak.Memory = usage.Memory
		}
		if usage.GPU > peak.GPU {
			peak.GPU = usage.GPU
		}
	}

	return peak
}

// Estimator predicts resource needs based on historical patterns.
type Estimator struct {
	histories map[string]*GroupHistory // key: namespace/groupName
	mu        sync.RWMutex
	logger    *slog.Logger
	maxSize   int
}

// NewEstimator creates a new resource estimator.
func NewEstimator(maxHistorySize int, logger *slog.Logger) *Estimator {
	if logger == nil {
		logger = slog.Default()
	}

	return &Estimator{
		histories: make(map[string]*GroupHistory),
		logger:    logger,
		maxSize:   maxHistorySize,
	}
}

// RecordUsage records resource usage for a group.
func (e *Estimator) RecordUsage(namespace, groupName string, cpu, memory, gpu float64) {
	key := fmt.Sprintf("%s/%s", namespace, groupName)

	e.mu.Lock()
	history, exists := e.histories[key]
	if !exists {
		history = NewGroupHistory(groupName, namespace, e.maxSize)
		e.histories[key] = history
	}
	e.mu.Unlock()

	history.AddUsage(cpu, memory, gpu)

	e.logger.Debug("recorded resource usage",
		"namespace", namespace,
		"group", groupName,
		"cpu", cpu,
		"memory", memory,
		"gpu", gpu,
	)
}

// EstimateResources predicts resource needs for a group.
// Uses weighted average: 70% average + 30% peak for safety margin.
func (e *Estimator) EstimateResources(namespace, groupName string) (corev1.ResourceList, error) {
	key := fmt.Sprintf("%s/%s", namespace, groupName)

	e.mu.RLock()
	history, exists := e.histories[key]
	e.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no history found for %s", key)
	}

	avg := history.GetAverage()
	peak := history.GetPeak()

	// Weighted estimation: 70% avg + 30% peak
	estimatedCPU := avg.CPU*0.7 + peak.CPU*0.3
	estimatedMem := avg.Memory*0.7 + peak.Memory*0.3
	estimatedGPU := avg.GPU*0.7 + peak.GPU*0.3

	resources := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(int64(estimatedCPU*1000), resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(int64(estimatedMem), resource.BinarySI),
	}

	if estimatedGPU > 0 {
		resources["nvidia.com/gpu"] = *resource.NewQuantity(int64(estimatedGPU), resource.DecimalSI)
	}

	e.logger.Info("estimated resources",
		"namespace", namespace,
		"group", groupName,
		"cpu", estimatedCPU,
		"memory", estimatedMem,
		"gpu", estimatedGPU,
	)

	return resources, nil
}

// GetHistory returns the history for a specific group.
func (e *Estimator) GetHistory(namespace, groupName string) (*GroupHistory, bool) {
	key := fmt.Sprintf("%s/%s", namespace, groupName)

	e.mu.RLock()
	defer e.mu.RUnlock()

	history, exists := e.histories[key]
	return history, exists
}

// CleanOldHistory removes histories older than the specified duration.
func (e *Estimator) CleanOldHistory(maxAge time.Duration) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	removed := 0
	cutoff := time.Now().Add(-maxAge)

	for key, history := range e.histories {
		history.mu.Lock()
		if len(history.History) > 0 && history.History[len(history.History)-1].Timestamp.Before(cutoff) {
			delete(e.histories, key)
			removed++
		}
		history.mu.Unlock()
	}

	e.logger.Info("cleaned old histories", "removed", removed)
	return removed
}
