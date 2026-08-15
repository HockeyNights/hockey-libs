package outbox

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	resultSent     = "sent"
	resultRetried  = "retried"
	resultDeferred = "deferred"
	resultFailed   = "failed"
)

type Metrics struct {
	pending  prometheus.Gauge
	failed   prometheus.Gauge
	messages *prometheus.CounterVec
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	metrics := &Metrics{
		pending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_pending_messages",
			Help: "Number of outbox messages waiting for delivery.",
		}),
		failed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_failed_messages",
			Help: "Number of outbox messages that exhausted their attempts.",
		}),
		messages: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "outbox_messages_total",
				Help: "Outbox delivery outcomes.",
			},
			[]string{"kind", "result"},
		),
	}

	registry.MustRegister(metrics.pending, metrics.failed, metrics.messages)

	return metrics
}

func (m *Metrics) observe(kind, result string) {
	if m == nil {
		return
	}

	m.messages.WithLabelValues(kind, result).Inc()
}

func (m *Metrics) depth(pending, failed int64) {
	if m == nil {
		return
	}

	m.pending.Set(float64(pending))
	m.failed.Set(float64(failed))
}
