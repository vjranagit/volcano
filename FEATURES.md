# New Features

This document describes the three major features added to the Volcano fork.

## 1. Admission Webhook with Validation

**Location:** `cmd/webhook`, `pkg/webhook`

### Description
Production-ready admission webhook that validates and mutates JobGroup CRDs before they're admitted to the cluster.

### Features
- **Validation:**
  - Ensures `minMember` is positive
  - Validates `maxMember >= minMember`
  - Requires `scheduleTimeoutSeconds` to be positive
  
- **Mutation (Default Values):**
  - Sets `maxMember = minMember * 2` if not specified
  - Sets default `priority = 50`
  - Sets default `scheduleTimeoutSeconds = 600`

### Usage
```bash
# Build
make build-webhook

# Run
./bin/webhook \
  --port=8443 \
  --cert-file=/etc/webhook/certs/tls.crt \
  --key-file=/etc/webhook/certs/tls.key
```

### Endpoints
- `POST /validate` - Validation webhook
- `POST /mutate` - Mutating webhook
- `GET /health` - Health check

### Example
```yaml
# Invalid JobGroup (will be rejected)
apiVersion: scheduling.volcano.sh/v1alpha1
kind: JobGroup
metadata:
  name: invalid
spec:
  minMember: 5
  maxMember: 3  # ERROR: maxMember < minMember

# Valid JobGroup (will have defaults applied)
apiVersion: scheduling.volcano.sh/v1alpha1
kind: JobGroup
metadata:
  name: valid
spec:
  minMember: 3
  # maxMember: 6 (will be set automatically)
  # priority: 50 (will be set automatically)
  # scheduleTimeoutSeconds: 600 (will be set automatically)
```

---

## 2. Enhanced Prometheus Metrics

**Location:** `pkg/metrics/collector.go`

### Description
Comprehensive Prometheus metrics for observability of the Volcano scheduler.

### Metrics Categories

#### Group Metrics
- `volcano_groups_total{state}` - Total number of job groups by state
- `volcano_group_ready_duration_seconds` - Time for a group to become ready
- `volcano_group_timeouts_total` - Total group timeouts
- `volcano_group_pods{group, namespace, phase}` - Pod count by phase

#### Quota Metrics
- `volcano_quota_allocated{namespace, resource}` - Allocated quota
- `volcano_quota_available{namespace, resource}` - Available quota
- `volcano_quota_borrowed{namespace, resource}` - Borrowed quota
- `volcano_quota_preemptions_total` - Total preemptions

#### Event Bus Metrics
- `volcano_events_published_total{type}` - Events published by type
- `volcano_events_dropped_total{type}` - Events dropped by type
- `volcano_event_bus_buffer_size` - Current buffer size

#### Scheduler Metrics
- `volcano_scheduling_attempts_total{result}` - Scheduling attempts
- `volcano_scheduling_latency_seconds` - Scheduling latency histogram

### Usage
```go
import "github.com/vjranagit/volcano/pkg/metrics"

collector := metrics.NewCollector(logger)

// Update metrics
collector.SetGroupsTotal("ready", 10)
collector.ObserveGroupReadyDuration(5.2)
collector.IncSchedulingAttempts("success")

// Start metrics server
go collector.ServeMetrics(":9090")
```

### Grafana Dashboard
Metrics are designed for easy integration with Grafana. Example queries:
```promql
# Group ready rate
rate(volcano_groups_total{state="ready"}[5m])

# P95 scheduling latency
histogram_quantile(0.95, volcano_scheduling_latency_seconds)

# Quota utilization
(volcano_quota_allocated / (volcano_quota_allocated + volcano_quota_available)) * 100
```

---

## 3. Predictive Resource Estimator

**Location:** `pkg/estimator/estimator.go`

### Description
Smart resource predictor that learns from historical patterns to optimize quota allocation and prevent over/under-provisioning.

### How It Works
1. **Records** resource usage (CPU, memory, GPU) for each job group
2. **Maintains** rolling history (configurable size, default 100 datapoints)
3. **Calculates** average and peak usage
4. **Predicts** future needs using weighted formula: `70% average + 30% peak`

### Features
- Thread-safe concurrent access
- FIFO eviction when history is full
- Automatic cleanup of old histories
- Support for custom resources (GPUs, etc.)

### Usage
```go
import "github.com/vjranagit/volcano/pkg/estimator"

est := estimator.NewEstimator(100, logger) // 100 datapoints per group

// Record usage (CPU cores, memory bytes, GPU count)
est.RecordUsage("default", "ml-training", 1500, 8192, 2)
est.RecordUsage("default", "ml-training", 1800, 9216, 2)
est.RecordUsage("default", "ml-training", 2000, 10240, 4)

// Get prediction
resources, err := est.EstimateResources("default", "ml-training")
// Returns: ResourceList with predicted CPU, memory, GPU

// Cleanup old data
removed := est.CleanOldHistory(7 * 24 * time.Hour) // Remove > 7 days old
```

### Example
```go
// Historical usage pattern:
// Run 1: 1000 CPU, 4GB RAM, 1 GPU
// Run 2: 1500 CPU, 6GB RAM, 2 GPU
// Run 3: 2000 CPU, 8GB RAM, 2 GPU

// Calculated:
// Average: 1500 CPU, 6GB, 1.67 GPU
// Peak:    2000 CPU, 8GB, 2 GPU

// Prediction (70% avg + 30% peak):
// CPU:    1650 cores
// Memory: 6.6GB
// GPU:    1.8 (rounds to 2)
```

### Integration with Quota Manager
```go
// Automatically allocate predicted resources
predicted, _ := estimator.EstimateResources(ns, group)
if quotaMgr.TryAllocate(predicted) {
    logger.Info("allocated predicted resources", "group", group)
}
```

### Benefits
- **Prevents resource waste** - No over-provisioning
- **Avoids starvation** - Safety margin from peak usage
- **Learns patterns** - Adapts to workload characteristics
- **Multi-tenancy friendly** - Per-group tracking

---

## Testing

All features include comprehensive unit tests:

```bash
# Test webhook
go test ./pkg/webhook/... -v

# Test metrics
go test ./pkg/metrics/... -v

# Test estimator
go test ./pkg/estimator/... -v

# Test everything
make test
```

## Production Readiness

All three features are production-ready:
- ✅ Full test coverage
- ✅ Thread-safe implementations
- ✅ Structured logging (slog)
- ✅ Error handling
- ✅ Documentation
- ✅ Performance optimized

## Future Enhancements

Potential improvements:
1. **Webhook:** Add support for custom validation rules via ConfigMap
2. **Metrics:** Add traces with OpenTelemetry integration
3. **Estimator:** Machine learning models for more sophisticated predictions
