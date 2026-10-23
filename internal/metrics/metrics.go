// Package metrics provides Prometheus metrics for PiWorker.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics labels
const (
	LabelFlowID   = "flow_id"
	LabelFlowName = "flow_name"
	LabelNodeID   = "node_id"
	LabelNodeType = "node_type"
	LabelStatus   = "status"
)

var (
	// FlowsTotal is the total number of flows in the system.
	FlowsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "piworker",
		Name:      "flows_total",
		Help:      "Total number of flows in the system",
	})

	// FlowsRunning is the number of currently running flows.
	FlowsRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "piworker",
		Name:      "flows_running",
		Help:      "Number of currently running flows",
	})

	// MessagesProcessed is the total number of messages processed.
	// Labelled by node_type (bounded by the registry) and status to keep
	// Prometheus cardinality finite — flow_id/node_id are per-UUID and would
	// grow the time-series set without bound, so they live in logs, not labels.
	MessagesProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "piworker",
		Name:      "messages_processed_total",
		Help:      "Total number of messages processed",
	}, []string{LabelNodeType, LabelStatus})

	// MessagesDropped is the total number of messages dropped due to backpressure.
	MessagesDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "piworker",
		Name:      "messages_dropped_total",
		Help:      "Total number of messages dropped due to backpressure",
	})

	// NodeExecutionDuration is a histogram of node execution times.
	NodeExecutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "piworker",
		Name:      "node_execution_duration_seconds",
		Help:      "Node execution duration in seconds",
		Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{LabelNodeType})

	// NodeExecutionErrors is the total number of node execution errors.
	NodeExecutionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "piworker",
		Name:      "node_execution_errors_total",
		Help:      "Total number of node execution errors",
	}, []string{LabelNodeType})

	// NodeRetries is the total number of node execution retries.
	NodeRetries = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "piworker",
		Name:      "node_retries_total",
		Help:      "Total number of node execution retries",
	}, []string{LabelNodeType})

	// FlowDeployments is the total number of flow deployments.
	FlowDeployments = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "piworker",
		Name:      "flow_deployments_total",
		Help:      "Total number of flow deployments",
	}, []string{LabelStatus})

	// HTTPRequestsTotal is the total number of HTTP requests.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "piworker",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration is a histogram of HTTP request durations.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "piworker",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// RecordMessageProcessed records a processed message for a node type.
func RecordMessageProcessed(nodeType string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	MessagesProcessed.WithLabelValues(nodeType, status).Inc()
}

// RecordNodeExecution records a node execution with its duration.
func RecordNodeExecution(nodeType string, durationSeconds float64) {
	NodeExecutionDuration.WithLabelValues(nodeType).Observe(durationSeconds)
}

// RecordNodeError records a node execution error.
func RecordNodeError(nodeType string) {
	NodeExecutionErrors.WithLabelValues(nodeType).Inc()
}

// RecordNodeRetry records a node execution retry.
func RecordNodeRetry(nodeType string) {
	NodeRetries.WithLabelValues(nodeType).Inc()
}

// RecordFlowDeployment records a flow deployment.
func RecordFlowDeployment(success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	FlowDeployments.WithLabelValues(status).Inc()
}

// RecordHTTPRequest records an HTTP request.
func RecordHTTPRequest(method, path string, statusCode int, durationSeconds float64) {
	status := statusCodeToLabel(statusCode)
	HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	HTTPRequestDuration.WithLabelValues(method, path).Observe(durationSeconds)
}

// SetFlowsTotal sets the total number of flows.
func SetFlowsTotal(count int) {
	FlowsTotal.Set(float64(count))
}

// SetFlowsRunning sets the number of running flows.
func SetFlowsRunning(count int) {
	FlowsRunning.Set(float64(count))
}

// IncrementFlowsRunning increments the number of running flows.
func IncrementFlowsRunning() {
	FlowsRunning.Inc()
}

// DecrementFlowsRunning decrements the number of running flows.
func DecrementFlowsRunning() {
	FlowsRunning.Dec()
}

// IncrementMessagesDropped increments the messages dropped counter.
func IncrementMessagesDropped() {
	MessagesDropped.Inc()
}

// statusCodeToLabel converts HTTP status codes to labels.
func statusCodeToLabel(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "other"
	}
}
