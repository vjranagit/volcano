package metrics

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	once     sync.Once
	registry *prometheus.Registry

	// Group metrics
	groupsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "volcano_groups_total",
			Help: "Total number of job groups by state",
		},
		[]string{"state"},
	)

	groupReadyDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "volcano_group_ready_duration_seconds",
			Help:    "Time taken for a group to become ready",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		},
	)

	groupTimeouts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "volcano_group_timeouts_total",
			Help: "Total number of group timeouts",
		},
	)

	groupPodsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "volcano_group_pods",
			Help: "Number of pods in groups by phase",
		},
		[]string{"group", "namespace", "phase"},
	)

	// Quota metrics
	quotaAllocated = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "volcano_quota_allocated",
			Help: "Allocated quota by namespace and resource",
		},
		[]string{"namespace", "resource"},
	)

	quotaAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "volcano_quota_available",
			Help: "Available quota by namespace and resource",
		},
		[]string{"namespace", "resource"},
	)

	quotaBorrowed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "volcano_quota_borrowed",
			Help: "Borrowed quota by namespace and resource",
		},
		[]string{"namespace", "resource"},
	)

	quotaPreemptions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "volcano_quota_preemptions_total",
			Help: "Total number of quota preemptions",
		},
	)

	// Event bus metrics
	eventsPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "volcano_events_published_total",
			Help: "Total number of events published by type",
		},
		[]string{"type"},
	)

	eventsDropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "volcano_events_dropped_total",
			Help: "Total number of events dropped by type",
		},
		[]string{"type"},
	)

	eventBusBufferSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "volcano_event_bus_buffer_size",
			Help: "Current size of event bus buffer",
		},
	)

	// Scheduler metrics
	schedulingAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "volcano_scheduling_attempts_total",
			Help: "Total scheduling attempts by result",
		},
		[]string{"result"},
	)

	schedulingLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "volcano_scheduling_latency_seconds",
			Help:    "Scheduling decision latency",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
	)
)

// Collector provides methods to update metrics.
type Collector struct {
	logger *slog.Logger
}

// NewCollector creates a new metrics collector.
func NewCollector(logger *slog.Logger) *Collector {
	once.Do(func() {
		registry = prometheus.NewRegistry()

		// Register all metrics
		registry.MustRegister(
			groupsTotal,
			groupReadyDuration,
			groupTimeouts,
			groupPodsGauge,
			quotaAllocated,
			quotaAvailable,
			quotaBorrowed,
			quotaPreemptions,
			eventsPublished,
			eventsDropped,
			eventBusBufferSize,
			schedulingAttempts,
			schedulingLatency,
		)
	})

	if logger == nil {
		logger = slog.Default()
	}

	return &Collector{logger: logger}
}

// Group metrics methods
func (c *Collector) SetGroupsTotal(state string, count float64) {
	groupsTotal.WithLabelValues(state).Set(count)
}

func (c *Collector) ObserveGroupReadyDuration(seconds float64) {
	groupReadyDuration.Observe(seconds)
}

func (c *Collector) IncGroupTimeouts() {
	groupTimeouts.Inc()
}

func (c *Collector) SetGroupPods(group, namespace, phase string, count float64) {
	groupPodsGauge.WithLabelValues(group, namespace, phase).Set(count)
}

// Quota metrics methods
func (c *Collector) SetQuotaAllocated(namespace, resource string, value float64) {
	quotaAllocated.WithLabelValues(namespace, resource).Set(value)
}

func (c *Collector) SetQuotaAvailable(namespace, resource string, value float64) {
	quotaAvailable.WithLabelValues(namespace, resource).Set(value)
}

func (c *Collector) SetQuotaBorrowed(namespace, resource string, value float64) {
	quotaBorrowed.WithLabelValues(namespace, resource).Set(value)
}

func (c *Collector) IncQuotaPreemptions() {
	quotaPreemptions.Inc()
}

// Event metrics methods
func (c *Collector) IncEventsPublished(eventType string) {
	eventsPublished.WithLabelValues(eventType).Inc()
}

func (c *Collector) IncEventsDropped(eventType string) {
	eventsDropped.WithLabelValues(eventType).Inc()
}

func (c *Collector) SetEventBusBufferSize(size float64) {
	eventBusBufferSize.Set(size)
}

// Scheduler metrics methods
func (c *Collector) IncSchedulingAttempts(result string) {
	schedulingAttempts.WithLabelValues(result).Inc()
}

func (c *Collector) ObserveSchedulingLatency(seconds float64) {
	schedulingLatency.Observe(seconds)
}

// ServeMetrics starts HTTP server for Prometheus metrics.
func (c *Collector) ServeMetrics(addr string) error {
	c.logger.Info("starting metrics server", "addr", addr)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return http.ListenAndServe(addr, mux)
}
