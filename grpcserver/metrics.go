package grpcserver

import (
	"context"
	"time"

	"github.com/HockeyNights/hockey-libs/observability"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight *prometheus.GaugeVec
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	metrics := &Metrics{
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "grpc_server_requests_total",
				Help: "Total number of gRPC requests handled by the server.",
			},
			[]string{"method", "code"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "grpc_server_request_duration_seconds",
				Help:    "Duration of gRPC requests handled by the server.",
				Buckets: observability.DurationBuckets,
			},
			[]string{"method"},
		),
		inFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "grpc_server_requests_in_flight",
				Help: "Number of gRPC requests currently being handled.",
			},
			[]string{"method"},
		),
	}

	registry.MustRegister(metrics.requests, metrics.duration, metrics.inFlight)

	return metrics
}

func (m *Metrics) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		m.inFlight.WithLabelValues(info.FullMethod).Inc()
		defer m.inFlight.WithLabelValues(info.FullMethod).Dec()

		start := time.Now()
		resp, err := handler(ctx, req)
		m.observe(info.FullMethod, time.Since(start), err)

		return resp, err
	}
}

func (m *Metrics) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		m.inFlight.WithLabelValues(info.FullMethod).Inc()
		defer m.inFlight.WithLabelValues(info.FullMethod).Dec()

		start := time.Now()
		err := handler(srv, stream)
		m.observe(info.FullMethod, time.Since(start), err)

		return err
	}
}

func (m *Metrics) observe(method string, duration time.Duration, err error) {
	m.requests.WithLabelValues(method, status.Code(err).String()).Inc()
	m.duration.WithLabelValues(method).Observe(duration.Seconds())
}
